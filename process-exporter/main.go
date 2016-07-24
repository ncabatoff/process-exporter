package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"strings"
	"time"

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

type (
	dummyResponseWriter struct {
		bytes.Buffer
		header http.Header
	}
)

func (d *dummyResponseWriter) Header() http.Header {
	return d.header
}

func (d *dummyResponseWriter) WriteHeader(code int) {
}

func main() {
	var (
		listenAddress = flag.String("web.listen-address", ":9256",
			"Address on which to expose metrics and web interface.")
		metricsPath = flag.String("web.telemetry-path", "/metrics",
			"Path under which to expose metrics.")
		onceToStdout = flag.Bool("once-to-stdout", false,
			"Don't bind, instead just print the metrics once to stdout and exit")
		procNames = flag.String("procnames", "",
			"comma-seperated list of process names to monitor")
		minIoPercent = flag.Float64("min-io-pct", 10.0,
			"percent of total I/O seen needed to promote out of 'other'")
		minCpuPercent = flag.Float64("min-cpu-pct", 10.0,
			"percent of total CPU seen needed to promote out of 'other'")
	)
	flag.Parse()

	var names []string
	for _, s := range strings.Split(*procNames, ",") {
		if s != "" {
			names = append(names, s)
		}
	}

	pc := NewProcessCollector(*minIoPercent, *minCpuPercent, names)

	if err := pc.Init(); err != nil {
		log.Fatalf("Error initializing: %v", err)
	}

	prometheus.MustRegister(pc)

	if *onceToStdout {
		drw := dummyResponseWriter{header: make(http.Header)}
		httpreq, err := http.NewRequest("GET", "/metrics", nil)
		if err != nil {
			log.Fatalf("Error building request: %v", err)
		}

		prometheus.Handler().ServeHTTP(&drw, httpreq)
		drw.Buffer.Truncate(0)

		// We throw away the first result because that first collection primes the pump, and
		// otherwise we won't see our counter metrics.  This is specific to the implementation
		// of NamedProcessCollector.Collect().
		prometheus.Handler().ServeHTTP(&drw, httpreq)
		fmt.Print(drw.String())
		return
	}

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
	NamedProcessCollector struct {
		minIoPercent  float64
		minCpuPercent float64
		wantProcNames map[string]struct{}
		// track how much was seen last time so we can report the delta
		groupStats map[string]counts
		tracker    *procTracker
	}

	counts struct {
		cpu        float64
		readbytes  uint64
		writebytes uint64
	}

	groupcounts struct {
		counts
		procs int
	}

	// procSum contains data read from /proc/pid/*
	procSum struct {
		name       string
		cmd        string
		cpu        float64
		readbytes  uint64
		writebytes uint64
		startTime  time.Time
	}

	trackedProc struct {
		lastUpdate time.Time
		lastvals   procSum
		accum      counts
	}

	processId struct {
		pid       int32
		startTime time.Time
	}

	procTracker struct {
		procs map[processId]trackedProc
		accum counts
	}
)

func (ps procSum) String() string {
	cmd := ps.cmd
	if len(cmd) > 20 {
		cmd = cmd[:20]
	}
	return fmt.Sprintf("%20s %20s %7.0f %12d %12d", ps.name, cmd, ps.cpu, ps.readbytes, ps.writebytes)
}

func NewProcessCollector(minIoPercent float64, minCpuPercent float64, procnames []string) *NamedProcessCollector {
	pc := NamedProcessCollector{
		minIoPercent:  minIoPercent,
		minCpuPercent: minCpuPercent,
		wantProcNames: make(map[string]struct{}),
		groupStats:    make(map[string]counts),
		tracker:       NewProcTracker(),
	}
	for _, name := range procnames {
		pc.wantProcNames[name] = struct{}{}
	}
	return &pc
}

func (p *NamedProcessCollector) Init() error {
	_, err := p.tracker.update()
	return err
}

// Describe implements prometheus.Collector.
func (p *NamedProcessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- cpusecsDesc
	ch <- numprocsDesc
	ch <- readbytesDesc
	ch <- writebytesDesc
}

// Collect implements prometheus.Collector.
func (p *NamedProcessCollector) Collect(ch chan<- prometheus.Metric) {
	for gname, gcounts := range p.getGroups() {
		ch <- prometheus.MustNewConstMetric(numprocsDesc,
			prometheus.GaugeValue, float64(gcounts.procs), gname)

		if grpstat, ok := p.groupStats[gname]; ok {
			// It's convenient to treat cpu, readbytes, etc as counters so we can use rate().
			// In practice it doesn't quite work because processes can exit while their group
			// persists, and with that pid's absence our summed value across the group will
			// become smaller.  We'll cheat by simply pretending there was no change to the
			// counter when that happens.

			dcpu := gcounts.cpu - grpstat.cpu
			if dcpu < 0 {
				dcpu = 0
			}
			ch <- prometheus.MustNewConstMetric(cpusecsDesc,
				prometheus.CounterValue,
				dcpu,
				gname)

			drbytes := gcounts.readbytes - grpstat.readbytes
			if drbytes < 0 {
				drbytes = 0
			}
			ch <- prometheus.MustNewConstMetric(readbytesDesc,
				prometheus.CounterValue,
				float64(drbytes),
				gname)

			dwbytes := gcounts.writebytes - grpstat.writebytes
			if dwbytes < 0 {
				dwbytes = 0
			}
			ch <- prometheus.MustNewConstMetric(writebytesDesc,
				prometheus.CounterValue,
				float64(dwbytes),
				gname)
		}

		p.groupStats[gname] = gcounts.counts
	}
}

func (p *NamedProcessCollector) getGroups() map[string]groupcounts {
	delta, err := p.tracker.update()
	if err != nil {
		log.Fatalf("Error reading procs: %v", err)
	}

	totdeltaio := float64(delta.readbytes + delta.writebytes)
	totdeltacpu := float64(delta.cpu + delta.cpu)
	gcounts := make(map[string]groupcounts)

	for _, pinfo := range p.tracker.procs {
		gname := pinfo.lastvals.name
		if _, ok := p.wantProcNames[gname]; !ok {
			deltaio := float64(pinfo.accum.readbytes + pinfo.accum.writebytes)
			iopct := 100 * deltaio / totdeltaio
			deltacpu := float64(pinfo.accum.readbytes + pinfo.accum.writebytes)
			cpupct := 100 * deltacpu / totdeltacpu
			if iopct >= p.minIoPercent || cpupct >= p.minCpuPercent/totdeltacpu {
				p.wantProcNames[gname] = struct{}{}
			} else {
				gname = "other"
			}
		}

		cur := gcounts[gname]
		cur.procs++
		cur.counts.cpu += pinfo.accum.cpu
		cur.counts.readbytes += pinfo.accum.readbytes
		cur.counts.writebytes += pinfo.accum.writebytes
		gcounts[gname] = cur
	}

	return gcounts
}

func NewProcTracker() *procTracker {
	return &procTracker{make(map[processId]trackedProc), counts{}}
}

// Scan /proc and update oneself.  Rather than allocating a new map each time to detect procs
// that have disappeared, we bump the last update time on those that are still present.  Then
// as a second pass we traverse the map looking for stale procs and removing them.

func (t procTracker) update() (delta counts, err error) {
	pids, err := process.Pids()
	if err != nil {
		return delta, fmt.Errorf("Error reading procs: %v", err)
	}

	now := time.Now()
	for _, pid := range pids {
		psum, err := getProcSummary(pid)
		if err != nil {
			continue
		}
		procid := processId{pid, psum.startTime}

		var newaccum counts
		if cur, ok := t.procs[procid]; ok {
			newaccum = cur.accum

			dcpu := psum.cpu - cur.lastvals.cpu
			newaccum.cpu += dcpu
			delta.cpu += dcpu

			drbytes := psum.readbytes - cur.lastvals.readbytes
			newaccum.readbytes += drbytes
			delta.readbytes += drbytes

			dwbytes := psum.writebytes - cur.lastvals.writebytes
			newaccum.writebytes += dwbytes
			delta.writebytes += dwbytes
			// log.Printf("%9d %20s %.1f %6d %6d", procid.pid, psum.name, dcpu, drbytes, dwbytes)
		}
		t.procs[procid] = trackedProc{lastUpdate: now, lastvals: psum, accum: newaccum}
	}

	for procid, pinfo := range t.procs {
		if pinfo.lastUpdate != now {
			delete(t.procs, procid)
		}
	}

	return delta, nil
}

var (
	ErrUnnamed   = errors.New("unnamed proc")
	ErrNoCommand = errors.New("proc has empty cmdline")
)

func getProcSummary(pid int32) (procSum, error) {
	proc, err := process.NewProcess(pid)
	var psum procSum
	if err != nil {
		// errors happens so routinely (e.g. when we race) that it's not worth reporting IMO
		return psum, err
	}

	times, err := proc.Times()
	if err != nil {
		return psum, err
	}

	name, err := proc.Name()
	if err != nil {
		return psum, err
	}

	if name == "" {
		// these all appear to be kernel processes, which people generally don't care about
		// monitoring directly (i.e. the system OS metrics suffice)
		return psum, ErrUnnamed
	}

	cmdline, err := proc.Cmdline()
	if err != nil {
		return psum, err
	}

	if cmdline == "" {
		// these all appear to be kernel processes, which people generally don't care about
		// monitoring directly (i.e. the system OS metrics suffice)
		return psum, ErrNoCommand
	}

	ios, err := proc.IOCounters()
	if err != nil {
		return psum, err
	}

	ctime, err := proc.CreateTime()
	if err != nil {
		return psum, err
	}

	psum.name = name
	psum.cmd = cmdline
	psum.cpu = times.User + times.System
	psum.writebytes = ios.WriteBytes
	psum.readbytes = ios.ReadBytes
	psum.startTime = time.Unix(ctime, 0)

	return psum, nil
}
