package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"regexp"
	"strings"

	"github.com/ncabatoff/fakescraper"
	"github.com/ncabatoff/process-exporter/proc"
	"github.com/prometheus/client_golang/prometheus"
)

func printManual() {
	fmt.Print(`process-exporter -procnames name1,...,nameN [options]

Every process not in the procnames list is ignored.  Otherwise, all processes
found are reported on as a group based on their shared name.  Here 'name' refers
to the value found in the second field of /proc/<pid>/stat.

The -namemapping option allows assigning a group name based on a combination of
the process name and command line.  For example, using 

  -namemapping "python2,([^/]+\.py),java,-jar\s+([^/+]).jar)" 

will make it so that each different python2 and java -jar invocation will be
tracked with distinct metrics.  Processes whose remapped name is absent from
the procnames list will be ignored.` + "\n")

}

var (
	numprocsDesc = prometheus.NewDesc(
		"namedprocess_namegroup_num_procs",
		"number of processes in this group",
		[]string{"groupname"},
		nil)

	CpusecsDesc = prometheus.NewDesc(
		"namedprocess_namegroup_cpu_seconds_total",
		"Cpu usage in seconds",
		[]string{"groupname"},
		nil)

	ReadBytesDesc = prometheus.NewDesc(
		"namedprocess_namegroup_read_bytes_total",
		"number of bytes read by this group",
		[]string{"groupname"},
		nil)

	WriteBytesDesc = prometheus.NewDesc(
		"namedprocess_namegroup_write_bytes_total",
		"number of bytes written by this group",
		[]string{"groupname"},
		nil)

	membytesDesc = prometheus.NewDesc(
		"namedprocess_namegroup_memory_bytes",
		"number of bytes of memory in use",
		[]string{"groupname", "memtype"},
		nil)
)

type (
	nameMapperRegex struct {
		mapping map[string]*regexp.Regexp
	}

	nameAndCmdline struct {
		name    string
		cmdline []string
	}

	namer interface {
		// Map returns the name to use for a given process
		Name(nameAndCmdline) string
	}
)

func (nm nameMapperRegex) Name(nacl nameAndCmdline) string {
	if regex, ok := nm.mapping[nacl.name]; ok {
		matches := regex.FindStringSubmatch(strings.Join(nacl.cmdline, " "))
		if len(matches) > 1 {
			for _, matchstr := range matches[1:] {
				if matchstr != "" {
					return nacl.name + ":" + matchstr
				}
			}
		}
	}
	return nacl.name
}

func parseNameMapper(s string) (*nameMapperRegex, error) {
	mapper := make(map[string]*regexp.Regexp)
	if s == "" {
		return &nameMapperRegex{mapper}, nil
	}

	toks := strings.Split(s, ",")
	if len(toks)%2 == 1 {
		return nil, fmt.Errorf("bad namemapper: odd number of tokens")
	}

	for i, tok := range toks {
		if tok == "" {
			return nil, fmt.Errorf("bad namemapper: token %d is empty", i)
		}
		if i%2 == 1 {
			name, regexstr := toks[i-1], tok
			if r, err := regexp.Compile(regexstr); err != nil {
				return nil, fmt.Errorf("error compiling regexp '%s': %v", regexstr, err)
			} else {
				mapper[name] = r
			}
		}
	}

	return &nameMapperRegex{mapper}, nil
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
		nameMapping = flag.String("namemapping", "",
			"comma-seperated list, alternating process name and capturing regex to apply to cmdline")
		man = flag.Bool("man", false,
			"print manual")
	)
	flag.Parse()

	if *man {
		printManual()
		return
	}

	var wantNames = make(map[string]struct{})
	for _, s := range strings.Split(*procNames, ",") {
		if s != "" {
			wantNames[s] = struct{}{}
		}
	}

	namemapper, err := parseNameMapper(*nameMapping)
	for name := range namemapper.mapping {
		wantNames[name] = struct{}{}
	}

	names := make([]string, 0, len(wantNames))
	for name := range wantNames {
		names = append(names, name)
	}
	log.Println(names)

	if err != nil {
		log.Fatalf("Error parsing -namemapping argument '%s': %v", *nameMapping, err)
	}

	pc := NewProcessCollector(names, namemapper)

	if err := pc.Init(); err != nil {
		log.Fatalf("Error initializing: %v", err)
	}

	prometheus.MustRegister(pc)

	if *onceToStdout {
		// We throw away the first result because that first collection primes the pump, and
		// otherwise we won't see our counter metrics.  This is specific to the implementation
		// of NamedProcessCollector.Collect().
		fs := fakescraper.NewFakeScraper()
		fs.Scrape()
		fmt.Print(fs.Scrape())
		return
	}

	http.Handle(*metricsPath, prometheus.Handler())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Named Process Exporter</title></head>
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
		wantProcNames map[string]struct{}
		namer
		// track how much was seen last time so we can report the delta
		groupStats map[string]proc.Counts
		tracker    *proc.Tracker
	}

	groupcounts struct {
		proc.Counts
		procs       int
		memresident uint64
		memvirtual  uint64
	}
)

func NewProcessCollector(procnames []string, n namer) *NamedProcessCollector {
	pc := NamedProcessCollector{
		wantProcNames: make(map[string]struct{}),
		namer:         n,
		groupStats:    make(map[string]proc.Counts),
		tracker:       proc.NewTracker(),
	}
	for _, name := range procnames {
		pc.wantProcNames[name] = struct{}{}
	}
	return &pc
}

func (p *NamedProcessCollector) Init() error {
	return p.update()
}

func (p *NamedProcessCollector) update() error {
	newProcs, err := p.tracker.Update(proc.AllProcs())
	if err != nil {
		return err
	}
	for _, idinfo := range newProcs {
		gname := p.namer.Name(nameAndCmdline{idinfo.Name, idinfo.Cmdline})
		if _, ok := p.wantProcNames[gname]; !ok {
			continue
		}

		p.tracker.Track(gname, idinfo)
	}
	for _, idinfo := range newProcs {
		ppid := idinfo.ParentPid
		pProcId := p.tracker.ProcIds[ppid]
		if tproc, ok := p.tracker.Tracked[pProcId]; ok {
			p.tracker.Track(tproc.GroupName, idinfo)
		}
	}
	return nil
}

// Describe implements prometheus.Collector.
func (p *NamedProcessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- CpusecsDesc
	ch <- numprocsDesc
	ch <- ReadBytesDesc
	ch <- WriteBytesDesc
	ch <- membytesDesc
}

// Collect implements prometheus.Collector.
func (p *NamedProcessCollector) Collect(ch chan<- prometheus.Metric) {
	counter := func(d *prometheus.Desc, val, prevVal float64, label string) {
		if val-prevVal < 0 {
			val = 0
		}
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, val, label)
	}

	for gname, gcounts := range p.getGroups() {
		ch <- prometheus.MustNewConstMetric(numprocsDesc,
			prometheus.GaugeValue, float64(gcounts.procs), gname)
		ch <- prometheus.MustNewConstMetric(membytesDesc,
			prometheus.GaugeValue, float64(gcounts.memresident), gname, "resident")
		ch <- prometheus.MustNewConstMetric(membytesDesc,
			prometheus.GaugeValue, float64(gcounts.memvirtual), gname, "virtual")

		if grpstat, ok := p.groupStats[gname]; ok {
			// It's convenient to treat Cpu, ReadBytes, etc as counters so we can use rate().
			// In practice it doesn't quite work because processes can exit while their group
			// persists, and with that pid's absence our summed value across the group will
			// become smaller.  We'll cheat by simply pretending there was no change to the
			// counter when that happens.

			counter(CpusecsDesc, gcounts.Cpu, grpstat.Cpu, gname)
			counter(ReadBytesDesc, float64(gcounts.ReadBytes), float64(grpstat.ReadBytes), gname)
			counter(WriteBytesDesc, float64(gcounts.WriteBytes), float64(grpstat.WriteBytes), gname)
		}

		p.groupStats[gname] = gcounts.Counts
	}
}

func (p *NamedProcessCollector) getGroups() map[string]groupcounts {
	err := p.update()
	if err != nil {
		log.Fatalf("Error reading procs: %v", err)
	}

	gcounts := make(map[string]groupcounts)

	for _, tinfo := range p.tracker.Tracked {
		cur := gcounts[tinfo.GroupName]
		cur.procs++
		counts, mem := tinfo.GetStats()
		cur.memresident += mem.Resident
		cur.memvirtual += mem.Virtual
		cur.Counts.Cpu += counts.Cpu
		cur.Counts.ReadBytes += counts.ReadBytes
		cur.Counts.WriteBytes += counts.WriteBytes
		gcounts[tinfo.GroupName] = cur
	}

	return gcounts
}
