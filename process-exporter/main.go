package main

import (
	"bytes"
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
	)
	flag.Parse()

	var names []string
	for _, s := range strings.Split(*procNames, ",") {
		if s != "" {
			names = append(names, s)
		}
	}

	pc := NewProcessCollector(*minIoPercent, names)

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
	ProcessCollector struct {
		minIoPercent  float64
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
	}
)

func (ps procSum) String() string {
	cmd := ps.cmd
	if len(cmd) > 20 {
		cmd = cmd[:20]
	}
	return fmt.Sprintf("%20s %20s %7.0f %12d %12d", ps.name, cmd, ps.cpu, ps.readbytes, ps.writebytes)
}

func NewProcessCollector(minIoPercent float64, procnames []string) *ProcessCollector {
	pc := ProcessCollector{
		minIoPercent:  minIoPercent,
		wantProcNames: make(map[string]struct{}),
		groupStats:    make(map[string]counts),
		tracker:       NewProcTracker(),
	}
	for _, name := range procnames {
		pc.wantProcNames[name] = struct{}{}
	}
	return &pc
}

func (p *ProcessCollector) Init() error {
	return p.tracker.update()
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

		p.groupStats[name] = counts{
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
		if psum, err := getProcSummary(pid); err != nil {
			// log.Printf("Error reading pid %d: %v", pid, err)
		} else {
			pname := psum.name
			// log.Printf("pname = %s", pname)
			if _, ok := p.wantProcNames[psum.name]; !ok {
				pname = "other"
			}
			procsums[pname] = append(procsums[pname], psum)
		}
	}

	return procsums
}

func NewProcTracker() *procTracker {
	return &procTracker{make(map[processId]trackedProc)}
}

// Scan /proc and update oneself.  Rather than allocating a new map each time to detect procs
// that have disappeared, we bump the last update time on those that are still present.  Then
// as a second pass we traverse the map looking for stale procs and removing them.

func (t procTracker) update() error {
	pids, err := process.Pids()
	if err != nil {
		return fmt.Errorf("Error reading procs: %v", err)
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
			newaccum.cpu += psum.cpu - cur.lastvals.cpu
			newaccum.readbytes += psum.readbytes - cur.lastvals.readbytes
			newaccum.writebytes += psum.writebytes - cur.lastvals.writebytes
		}
		t.procs[procid] = trackedProc{now, psum, newaccum}
	}
	return nil
}

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

	cmdline, err := proc.Cmdline()
	if err != nil {
		return psum, err
	}

	if cmdline == "" {
		// these all appear to be kernel processes, which people generally don't care about
		// monitoring directly (i.e. the system OS metrics suffice)
		return psum, err
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
