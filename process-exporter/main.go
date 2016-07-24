package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"strings"

	// "github.com/ncabatoff/gopsutil/process" // use my fork until shirou/gopsutil issue#235 fixed, but needs branch fix-internal-pkgref
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/process"
)

var (
	numprocsDesc = prometheus.NewDesc(
		"namedprocess_namegroup_num_procs",
		"number of processes in this group",
		[]string{"groupname"},
		nil)

	cpusecsDesc = prometheus.NewDesc(
		"namedprocess_namegroup_cpu_seconds_total",
		"cpu usage in seconds",
		[]string{"groupname"},
		nil)

	readbytesDesc = prometheus.NewDesc(
		"namedprocess_namegroup_read_bytes_total",
		"number of bytes read by this group",
		[]string{"groupname"},
		nil)

	writebytesDesc = prometheus.NewDesc(
		"namedprocess_namegroup_write_bytes_total",
		"number of bytes written by this group",
		[]string{"groupname"},
		nil)
)

func main() {
	var (
		listenAddress = flag.String("web.listen-address", ":9256", "Address on which to expose metrics and web interface.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		procNames     = flag.String("procnames", "", "comma-seperated list of process names to monitor")
	)
	flag.Parse()

	prometheus.MustRegister(NewProcessCollector(strings.Split(*procNames, ",")))

	http.Handle(*metricsPath, prometheus.Handler())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Sensor Exporter</title></head>
			<body>
			<h1>Named Process Exporter</h1>
			<p><a href="` + *metricsPath + `">Metrics</a></p>
			</body>
			</html>`))
	})
	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		log.Fatalf("Unable to setup HTTP server: %v", err)
	}
}

type (
	ProcessCollector struct {
		wantProcNames map[string]struct{}
		// track how much was seen last time so we can report the delta
		groupStats map[string]groupStat
	}

	groupStat struct {
		cpu        float64
		readbytes  uint64
		writebytes uint64
	}

	procSum struct {
		pid        int32
		name       string
		cmd        string
		cpu        float64
		readbytes  uint64
		writebytes uint64
	}
)

func (ps procSum) String() string {
	cmd := ps.cmd
	if len(cmd) > 20 {
		cmd = cmd[:20]
	}
	return fmt.Sprintf("%20s %20s %7.0f %12d %12d", ps.name, cmd, ps.cpu, ps.readbytes, ps.writebytes)
}

func NewProcessCollector(procnames []string) *ProcessCollector {
	pc := ProcessCollector{
		wantProcNames: make(map[string]struct{}),
		groupStats:    make(map[string]groupStat),
	}
	for _, name := range procnames {
		pc.wantProcNames[name] = struct{}{}
	}
	return &pc
}

// Describe implements prometheus.Collector.
func (p *ProcessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- cpusecsDesc
	ch <- numprocsDesc
	ch <- readbytesDesc
	ch <- writebytesDesc
}

// Collect implements prometheus.Collector.
func (p *ProcessCollector) Collect(ch chan<- prometheus.Metric) {
	for name, pss := range p.getGroups() {
		ch <- prometheus.MustNewConstMetric(numprocsDesc,
			prometheus.GaugeValue, float64(len(pss)), name)

		var cpusecs float64
		var rbytes, wbytes uint64
		for _, ps := range pss {
			cpusecs += ps.cpu
			rbytes += ps.readbytes
			wbytes += ps.writebytes
		}

		if grpstat, ok := p.groupStats[name]; ok {
			// It's convenient to treat cpu, readbytes, etc as counters so we can use rate().
			// In practice it doesn't quite work because processes can exit while their group
			// persists, and with that pid's absence our summed value across the group will
			// become smaller.  We'll cheat by simply pretending there was no change to the
			// counter when that happens.

			dcpu := cpusecs - grpstat.cpu
			if dcpu < 0 {
				dcpu = 0
			}
			ch <- prometheus.MustNewConstMetric(cpusecsDesc,
				prometheus.CounterValue,
				dcpu,
				name)

			drbytes := rbytes - grpstat.readbytes
			if drbytes < 0 {
				drbytes = 0
			}
			ch <- prometheus.MustNewConstMetric(readbytesDesc,
				prometheus.CounterValue,
				float64(drbytes),
				name)

			dwbytes := wbytes - grpstat.writebytes
			if dwbytes < 0 {
				dwbytes = 0
			}
			ch <- prometheus.MustNewConstMetric(writebytesDesc,
				prometheus.CounterValue,
				float64(dwbytes),
				name)
		}

		p.groupStats[name] = groupStat{
			cpu:        cpusecs,
			readbytes:  rbytes,
			writebytes: wbytes,
		}
	}
}

func (p *ProcessCollector) getGroups() map[string][]procSum {
	procsums := make(map[string][]procSum)

	pids, err := process.Pids()
	if err != nil {
		log.Fatalf("Error reading procs: %v", err)
	}

	for _, pid := range pids {
		if psum, interesting := p.isProcInteresting(pid); interesting {
			pname := psum.name
			if _, ok := p.wantProcNames[psum.name]; !ok {
				pname = "other"
			}
			procsums[pname] = append(procsums[pname], psum)
		}
	}

	return procsums
}

func (p *ProcessCollector) isProcInteresting(pid int32) (psum procSum, interesting bool) {
	proc, err := process.NewProcess(pid)
	if err != nil {
		// errors happens so routinely (e.g. when we race) that it's not worth reporting IMO
		return
	}

	times, err := proc.Times()
	if times.User+times.System < 0.1 {
		return
	}

	name, err := proc.Name()
	if err != nil {
		return
	}

	cmdline, err := proc.Cmdline()
	if err != nil {
		return
	}

	if cmdline == "" {
		// these all appear to be kernel processes, which people generally don't care about
		// monitoring directly (i.e. the system OS metrics suffice)
		return
	}

	ios, err := proc.IOCounters()
	if err != nil {
		return
	}

	psum.pid = pid
	psum.name = name
	psum.cmd = cmdline
	psum.cpu = times.User + times.System
	psum.writebytes = ios.WriteBytes
	psum.readbytes = ios.ReadBytes

	return psum, true
}
