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
found are reported on as a group based on the process name they share. 
Here 'process name' refers to the value found in the second field of
/proc/<pid>/stat, which is truncated at 15 chars.

The -children option makes it so that any process that otherwise isn't part of
its own group becomes part of the first group found (if any) when walking the
process tree upwards.  In other words, subprocesses resource usage gets
accounted for as part of their parents.  This is the default behaviour.

The -namemapping option allows assigning a group name based on a combination of
the process name and command line.  For example, using 

  -namemapping "python2,([^/]+\.py),java,-jar\s+([^/+]).jar)" 

will make it so that each different python2 and java -jar invocation will be
tracked with distinct metrics.  Processes whose remapped name is absent from
the procnames list will be ignored.  Here's an example that I run on my home
machine (Ubuntu Xenian):

  process-exporter -namemapping "upstart,(--user)" \
    -procnames chromium-browse,bash,prometheus,prombench,gvim,upstart:-user

Since it appears that upstart --user is the parent process of my X11 session,
this will make all apps I start count against it, unless they're one of the
others named explicitly with -procnames.

` + "\n")

}

var (
	numprocsDesc = prometheus.NewDesc(
		"namedprocess_namegroup_num_procs",
		"number of processes in this group",
		[]string{"groupname"},
		nil)

	cpuSecsDesc = prometheus.NewDesc(
		"namedprocess_namegroup_cpu_seconds_total",
		"Cpu usage in seconds",
		[]string{"groupname"},
		nil)

	readBytesDesc = prometheus.NewDesc(
		"namedprocess_namegroup_read_bytes_total",
		"number of bytes read by this group",
		[]string{"groupname"},
		nil)

	writeBytesDesc = prometheus.NewDesc(
		"namedprocess_namegroup_write_bytes_total",
		"number of bytes written by this group",
		[]string{"groupname"},
		nil)

	membytesDesc = prometheus.NewDesc(
		"namedprocess_namegroup_memory_bytes",
		"number of bytes of memory in use",
		[]string{"groupname", "memtype"},
		nil)

	scrapeErrorsDesc = prometheus.NewDesc(
		"namedprocess_scrape_errors",
		"non-permission scrape errors",
		nil,
		nil)

	scrapePermissionErrorsDesc = prometheus.NewDesc(
		"namedprocess_scrape_permission_errors",
		"permission scrape errors (unreadable files under /proc)",
		nil,
		nil)
)

type (
	nameMapperRegex struct {
		mapping map[string]*regexp.Regexp
	}
)

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
		procfsPath = flag.String("procfs", "/proc",
			"path to read proc data from")
		nameMapping = flag.String("namemapping", "",
			"comma-seperated list, alternating process name and capturing regex to apply to cmdline")
		children = flag.Bool("children", true,
			"if a proc is tracked, track with it any children that aren't part of their own group")
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
	log.Printf("Reading metrics from %s for procnames: %v", *procfsPath, names)

	if err != nil {
		log.Fatalf("Error parsing -namemapping argument '%s': %v", *nameMapping, err)
	}

	pc, err := NewProcessCollector(*procfsPath, names, *children, namemapper)
	if err != nil {
		log.Fatalf("Error initializing: %v", err)
	}

	prometheus.MustRegister(pc)

	if *onceToStdout {
		// We throw away the first result because that first collection primes the pump, and
		// otherwise we won't see our counter metrics.  This is specific to the implementation
		// of NamedProcessCollector.Collect().
		fscraper := fakescraper.NewFakeScraper()
		fscraper.Scrape()
		fmt.Print(fscraper.Scrape())
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
		*proc.Grouper
		fs                     *proc.FS
		scrapeErrors           int
		scrapePermissionErrors int
	}
)

func (nm nameMapperRegex) Name(nacl proc.NameAndCmdline) string {
	if regex, ok := nm.mapping[nacl.Name]; ok {
		matches := regex.FindStringSubmatch(strings.Join(nacl.Cmdline, " "))
		if len(matches) > 1 {
			for _, matchstr := range matches[1:] {
				if matchstr != "" {
					return nacl.Name + ":" + matchstr
				}
			}
		}
	}
	return nacl.Name
}

func NewProcessCollector(
	procfsPath string,
	procnames []string,
	children bool,
	n proc.Namer,
) (*NamedProcessCollector, error) {
	fs, err := proc.NewFS(procfsPath)
	if err != nil {
		return nil, err
	}
	p := &NamedProcessCollector{
		Grouper: proc.NewGrouper(procnames, children, n),
		fs:      fs,
	}

	_, err = p.Update(p.fs.AllProcs())
	if err != nil {
		return nil, err
	}

	return p, nil
}

// Describe implements prometheus.Collector.
func (p *NamedProcessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- cpuSecsDesc
	ch <- numprocsDesc
	ch <- readBytesDesc
	ch <- writeBytesDesc
	ch <- membytesDesc
	ch <- scrapeErrorsDesc
	ch <- scrapePermissionErrorsDesc
}

// Collect implements prometheus.Collector.
func (p *NamedProcessCollector) Collect(ch chan<- prometheus.Metric) {
	permErrs, err := p.Update(p.fs.AllProcs())
	p.scrapePermissionErrors += permErrs
	if err != nil {
		p.scrapeErrors++
		log.Printf("error reading procs: %v", err)
	} else {
		for gname, gcounts := range p.Groups() {
			ch <- prometheus.MustNewConstMetric(numprocsDesc,
				prometheus.GaugeValue, float64(gcounts.Procs), gname)
			ch <- prometheus.MustNewConstMetric(membytesDesc,
				prometheus.GaugeValue, float64(gcounts.Memresident), gname, "resident")
			ch <- prometheus.MustNewConstMetric(membytesDesc,
				prometheus.GaugeValue, float64(gcounts.Memvirtual), gname, "virtual")
			ch <- prometheus.MustNewConstMetric(cpuSecsDesc,
				prometheus.CounterValue, gcounts.Cpu, gname)
			ch <- prometheus.MustNewConstMetric(readBytesDesc,
				prometheus.CounterValue, float64(gcounts.ReadBytes), gname)
			ch <- prometheus.MustNewConstMetric(writeBytesDesc,
				prometheus.CounterValue, float64(gcounts.WriteBytes), gname)
		}
	}
	ch <- prometheus.MustNewConstMetric(scrapeErrorsDesc,
		prometheus.CounterValue, float64(p.scrapeErrors))
	ch <- prometheus.MustNewConstMetric(scrapePermissionErrorsDesc,
		prometheus.CounterValue, float64(p.scrapePermissionErrors))
}
