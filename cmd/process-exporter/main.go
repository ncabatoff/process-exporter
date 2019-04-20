package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ncabatoff/fakescraper"
	common "github.com/ncabatoff/process-exporter"
	"github.com/ncabatoff/process-exporter/config"
	"github.com/ncabatoff/process-exporter/proc"
	"github.com/prometheus/client_golang/prometheus"
)

// Version is set at build time use ldflags.
var version string

func printManual() {
	fmt.Print(`Usage:
  process-exporter [options] -config.path filename.yml

or 

  process-exporter [options] -procnames name1,...,nameN [-namemapping k1,v1,...,kN,vN]

The recommended option is to use a config file, but for convenience and
backwards compatibility the -procnames/-namemapping options exist as an
alternative.
 
The -children option (default:true) makes it so that any process that otherwise
isn't part of its own group becomes part of the first group found (if any) when
walking the process tree upwards.  In other words, resource usage of
subprocesses is added to their parent's usage unless the subprocess identifies
as a different group name.

Command-line process selection (procnames/namemapping):

  Every process not in the procnames list is ignored.  Otherwise, all processes
  found are reported on as a group based on the process name they share. 
  Here 'process name' refers to the value found in the second field of
  /proc/<pid>/stat, which is truncated at 15 chars.

  The -namemapping option allows assigning a group name based on a combination of
  the process name and command line.  For example, using 

    -namemapping "python2,([^/]+)\.py,java,-jar\s+([^/]+).jar" 

  will make it so that each different python2 and java -jar invocation will be
  tracked with distinct metrics.  Processes whose remapped name is absent from
  the procnames list will be ignored.  Here's an example that I run on my home
  machine (Ubuntu Xenian):

    process-exporter -namemapping "upstart,(--user)" \
      -procnames chromium-browse,bash,prometheus,prombench,gvim,upstart:-user

  Since it appears that upstart --user is the parent process of my X11 session,
  this will make all apps I start count against it, unless they're one of the
  others named explicitly with -procnames.

Config file process selection (filename.yml):

  See README.md.
` + "\n")

}

var (
	desc = descriptors{
		"numprocs": {
			"namedprocess_namegroup_num_procs",
			"number of processes in this group",
			prometheus.GaugeValue,
			[]string{"groupname"},
			nil},

		"cpuSecs": {
			"namedprocess_namegroup_cpu_seconds_total",
			"Cpu user usage in seconds",
			prometheus.CounterValue,
			[]string{"groupname", "mode"},
			nil},

		"readBytes": {
			"namedprocess_namegroup_read_bytes_total",
			"number of bytes read by this group",
			prometheus.CounterValue,
			[]string{"groupname"},
			nil},

		"writeBytes": {
			"namedprocess_namegroup_write_bytes_total",
			"number of bytes written by this group",
			prometheus.CounterValue,
			[]string{"groupname"},
			nil},

		"majorPageFaults": {
			"namedprocess_namegroup_major_page_faults_total",
			"Major page faults",
			prometheus.CounterValue,
			[]string{"groupname"},
			nil},

		"minorPageFaults": {
			"namedprocess_namegroup_minor_page_faults_total",
			"Minor page faults",
			prometheus.CounterValue,
			[]string{"groupname"},
			nil},

		"contextSwitches": {
			"namedprocess_namegroup_context_switches_total",
			"Context switches",
			prometheus.CounterValue,
			[]string{"groupname", "ctxswitchtype"},
			nil},

		"membytes": {
			"namedprocess_namegroup_memory_bytes",
			"number of bytes of memory in use",
			prometheus.GaugeValue,
			[]string{"groupname", "memtype"},
			nil},

		"openFDs": {
			"namedprocess_namegroup_open_filedesc",
			"number of open file descriptors for this group",
			prometheus.GaugeValue,
			[]string{"groupname"},
			nil},

		"worstFDRatio": {
			"namedprocess_namegroup_worst_fd_ratio",
			"the worst (closest to 1) ratio between open fds and max fds among all procs in this group",
			prometheus.GaugeValue,
			[]string{"groupname"},
			nil},

		"startTime": {
			"namedprocess_namegroup_oldest_start_time_seconds",
			"start time in seconds since 1970/01/01 of oldest process in group",
			prometheus.GaugeValue,
			[]string{"groupname"},
			nil},

		"numThreads": {
			"namedprocess_namegroup_num_threads",
			"Number of threads",
			prometheus.GaugeValue,
			[]string{"groupname"},
			nil},

		"states": {
			"namedprocess_namegroup_states",
			"Number of processes in states Running, Sleeping, Waiting, Zombie, or Other",
			prometheus.GaugeValue,
			[]string{"groupname", "state"},
			nil},

		"scrapeErrors": {
			"namedprocess_scrape_errors",
			"general scrape errors: no proc metrics collected during a cycle",
			prometheus.CounterValue,
			nil,
			nil},

		"scrapeProcReadErrors": {
			"namedprocess_scrape_procread_errors",
			"incremented each time a proc's metrics collection fails",
			prometheus.CounterValue,
			nil,
			nil},

		"scrapePartialErrors": {
			"namedprocess_scrape_partial_errors",
			"incremented each time a tracked proc's metrics collection fails partially, e.g. unreadable I/O stats",
			prometheus.CounterValue,
			nil,
			nil},

		"threadWchan": {
			"namedprocess_namegroup_threads_wchan",
			"Number of threads in this group waiting on each wchan",
			prometheus.GaugeValue,
			[]string{"groupname", "wchan"},
			nil},

		"threadCount": {
			"namedprocess_namegroup_thread_count",
			"Number of threads in this group with same threadname",
			prometheus.GaugeValue,
			[]string{"groupname", "threadname"},
			nil},

		"threadCpuSecs": {
			"namedprocess_namegroup_thread_cpu_seconds_total",
			"Cpu user/system usage in seconds",
			prometheus.CounterValue,
			[]string{"groupname", "threadname", "mode"},
			nil},

		"threadIoBytes": {
			"namedprocess_namegroup_thread_io_bytes_total",
			"number of bytes read/written by these threads",
			prometheus.CounterValue,
			[]string{"groupname", "threadname", "iomode"},
			nil},

		"threadMajorPageFaults": {
			"namedprocess_namegroup_thread_major_page_faults_total",
			"Major page faults for these threads",
			prometheus.CounterValue,
			[]string{"groupname", "threadname"},
			nil},

		"threadMinorPageFaults": {
			"namedprocess_namegroup_thread_minor_page_faults_total",
			"Minor page faults for these threads",
			prometheus.CounterValue,
			[]string{"groupname", "threadname"},
			nil},

		"threadContextSwitches": {
			"namedprocess_namegroup_thread_context_switches_total",
			"Context switches for these threads",
			prometheus.CounterValue,
			[]string{"groupname", "threadname", "ctxswitchtype"},
			nil},
	}
	// global variables for now
	showUser, showPod *bool
	podDefaultLabel   *string
)

type (
	descriptor struct {
		name    string
		help    string
		valType prometheus.ValueType
		labels  []string
		desc    *prometheus.Desc
	}

	descriptors map[string]*descriptor

	prefixRegex struct {
		prefix string
		regex  *regexp.Regexp
	}

	nameMapperRegex struct {
		mapping   map[string]*prefixRegex
		resolvers []common.Resolver
	}
)

func (d descriptor) hasLabel(label string) bool {
	for _, l := range d.labels {
		if l == label {
			return true
		}
	}
	return false
}

func (dd descriptors) addGroupLabel(label string) {
	for idx, d := range dd {
		if d.hasLabel("groupname") {
			dd[idx].labels = append(d.labels, label)
		}
	}
}

// metric creates prometheus Metric with labels from labelMap. If some defined label is missing in labelMap, it's filled with lastLabel value
func (d *descriptor) metric(value float64, labelMap map[string]string, lastLabel string) prometheus.Metric {
	var labels []string
	for _, l := range d.labels {
		val, ok := labelMap[l]
		if !ok {
			val = lastLabel
		}
		labels = append(labels, val)
	}
	return prometheus.MustNewConstMetric(d.desc, d.valType, value, labels...)
}

func (dd descriptors) init() {
	for idx, d := range dd {
		dd[idx].desc = prometheus.NewDesc(d.name, d.help, d.labels, nil)
	}
}

func (nmr *nameMapperRegex) String() string {
	return fmt.Sprintf("%+v", nmr.mapping)
}

// Create a nameMapperRegex based on a string given as the -namemapper argument.
func parseNameMapper(s string) (*nameMapperRegex, error) {
	mapper := make(map[string]*prefixRegex)
	if s == "" {
		return &nameMapperRegex{mapper, nil}, nil
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
			matchName := name
			prefix := name + ":"

			if r, err := regexp.Compile(regexstr); err != nil {
				return nil, fmt.Errorf("error compiling regexp '%s': %v", regexstr, err)
			} else {
				mapper[matchName] = &prefixRegex{prefix: prefix, regex: r}
			}
		}
	}

	return &nameMapperRegex{mapper, nil}, nil
}

// AddResolver implements common.MatchNamer interface
func (nmr *nameMapperRegex) AddResolver(resolver common.Resolver) {
	nmr.resolvers = append(nmr.resolvers, resolver)
}

func (nmr *nameMapperRegex) labels(nacl common.ProcAttributes) string {
	ret := ""
	if !*showUser && !*showPod {
		return ret
	}
	for _, res := range nmr.resolvers {
		res.Resolve(&nacl)
	}
	if *showUser {
		ret += "user:" + nacl.Username + ";"
	}
	if *showPod {
		if nacl.Pod == "" {
			ret += "pod:" + *podDefaultLabel + ";"
		} else {
			ret += "pod:" + nacl.Pod + ";"
		}
	}
	return ret
}

func (nmr *nameMapperRegex) MatchAndName(nacl common.ProcAttributes) (bool, string) {
	if pregex, ok := nmr.mapping[nacl.Name]; ok {
		if pregex == nil {
			return true, nmr.labels(nacl) + nacl.Name
		}
		matches := pregex.regex.FindStringSubmatch(strings.Join(nacl.Cmdline, " "))
		if len(matches) > 1 {
			for _, matchstr := range matches[1:] {
				if matchstr != "" {
					return true, nmr.labels(nacl) + pregex.prefix + matchstr
				}
			}
		}
	}
	return false, ""
}

func main() {
	var (
		listenAddress = flag.String("web.listen-address", ":9256",
			"Address on which to expose metrics and web interface.")
		metricsPath = flag.String("web.telemetry-path", "/metrics",
			"Path under which to expose metrics.")
		onceToStdoutDelay = flag.Duration("once-to-stdout-delay", 0,
			"Don't bind, just wait this much time, print the metrics once to stdout, and exit")
		procNames = flag.String("procnames", "",
			"comma-separated list of process names to monitor")
		procfsPath = flag.String("procfs", "/proc",
			"path to read proc data from")
		nameMapping = flag.String("namemapping", "",
			"comma-separated list, alternating process name and capturing regex to apply to cmdline")
		children = flag.Bool("children", true,
			"if a proc is tracked, track with it any children that aren't part of their own group")
		threads = flag.Bool("threads", true,
			"report on per-threadname metrics as well")
		man = flag.Bool("man", false,
			"print manual")
		configPath = flag.String("config.path", "",
			"path to YAML config file")
		recheck = flag.Bool("recheck", false,
			"recheck process names on each scrape")
		debug = flag.Bool("debug", false,
			"log debugging information to stdout")
		template = flag.String("template", "{{index .Config.Labels \"io.kubernetes.pod.name\"}}",
			"go template for 'docker inspect --format' command to receive POD names")
		showVersion = flag.Bool("version", false,
			"print version information and exit")
	)
	showPod = flag.Bool("pod", false,
		"append 'pod' label to metrics, filled with pod name if process is executed in docker")
	podDefaultLabel = flag.String("pod-default-label", "",
		"default 'pod' label for processes executed not in docker")
	showUser = flag.Bool("user", false,
		"append 'user' label to metrics")

	flag.Parse()

	if *showVersion {
		fmt.Printf("process-exporter version %s\n", version)
		os.Exit(0)
	}

	if *man {
		printManual()
		return
	}

	// append 'pod' label to descs
	// See MatchAndName how to add another labels
	// Don't forget to add all possible labels to map in scrape function
	if *showPod {
		desc.addGroupLabel("pod")
	}

	if *showUser {
		desc.addGroupLabel("user")
	}

	// init prometheus.Desc variables from internal internal desc structs
	desc.init()

	var matchnamer common.MatchNamer

	if *configPath != "" {
		if *nameMapping != "" || *procNames != "" {
			log.Fatalf("-config.path cannot be used with -namemapping or -procnames")
		}

		cfg, err := config.ReadFile(*configPath, *debug)
		if err != nil {
			log.Fatalf("error reading config file %q: %v", *configPath, err)
		}
		log.Printf("Reading metrics from %s based on %q", *procfsPath, *configPath)
		matchnamer = cfg.MatchNamers
		if *debug {
			log.Printf("using config matchnamer: %v", cfg.MatchNamers)
		}
	} else {
		namemapper, err := parseNameMapper(*nameMapping)
		if err != nil {
			log.Fatalf("Error parsing -namemapping argument '%s': %v", *nameMapping, err)
		}

		var names []string
		for _, s := range strings.Split(*procNames, ",") {
			if s != "" {
				if _, ok := namemapper.mapping[s]; !ok {
					namemapper.mapping[s] = nil
				}
				names = append(names, s)
			}
		}

		log.Printf("Reading metrics from %s for procnames: %v", *procfsPath, names)
		if *debug {
			log.Printf("using cmdline matchnamer: %v", namemapper)
		}
		matchnamer = namemapper
	}
	// add additional resolvers in the same way
	if *showPod {
		matchnamer.AddResolver(proc.NewDockerResolver(*debug, *template))
	}

	pc, err := NewProcessCollector(*procfsPath, *children, *threads, matchnamer, *recheck, *debug)
	if err != nil {
		log.Fatalf("Error initializing: %v", err)
	}

	prometheus.MustRegister(pc)

	if *onceToStdoutDelay != 0 {
		// We throw away the first result because that first collection primes the pump, and
		// otherwise we won't see our counter metrics.  This is specific to the implementation
		// of NamedProcessCollector.Collect().
		fscraper := fakescraper.NewFakeScraper()
		fscraper.Scrape()
		time.Sleep(*onceToStdoutDelay)
		fmt.Print(fscraper.Scrape())
		return
	}
	if *debug {
		log.Println("Starting...")
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
	scrapeRequest struct {
		results chan<- prometheus.Metric
		done    chan struct{}
	}

	// NamedProcessCollector ...
	NamedProcessCollector struct {
		scrapeChan chan scrapeRequest
		*proc.Grouper
		threads              bool
		source               proc.Source
		scrapeErrors         int
		scrapeProcReadErrors int
		scrapePartialErrors  int
		debug                bool
	}
)

// NewProcessCollector ...
func NewProcessCollector(
	procfsPath string,
	children bool,
	threads bool,
	n common.MatchNamer,
	recheck bool,
	debug bool,
) (*NamedProcessCollector, error) {
	fs, err := proc.NewFS(procfsPath, debug)
	if err != nil {
		return nil, err
	}

	p := &NamedProcessCollector{
		scrapeChan: make(chan scrapeRequest),
		Grouper:    proc.NewGrouper(n, children, threads, recheck, debug),
		source:     fs,
		threads:    threads,
		debug:      debug,
	}

	colErrs, _, err := p.Update(p.source.AllProcs())
	if err != nil {
		if debug {
			log.Print(err)
		}
		return nil, err
	}
	p.scrapePartialErrors += colErrs.Partial
	p.scrapeProcReadErrors += colErrs.Read

	go p.start()

	return p, nil
}

// Describe implements prometheus.Collector.
func (p *NamedProcessCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range desc {
		ch <- d.desc
	}
}

// Collect implements prometheus.Collector.
func (p *NamedProcessCollector) Collect(ch chan<- prometheus.Metric) {
	req := scrapeRequest{results: ch, done: make(chan struct{})}
	p.scrapeChan <- req
	<-req.done
}

func (p *NamedProcessCollector) start() {
	for req := range p.scrapeChan {
		ch := req.results
		p.scrape(ch)
		req.done <- struct{}{}
	}
}

// parse string in format "label1:value1;label2: ...:valueN;rest-of-string"
// Returns map whith map[restname] = rest-of-string
// For sure labels and their values (except of rest) can't contain : and ;
func parse(str string, labels []string, restname string) map[string]string {
	max := -1
	ret := make(map[string]string)
	for _, label := range labels {
		start := strings.Index(str, label+":")
		l := len(label)
		if start > -1 {
			s := str[start:]
			div := strings.Index(s, ";")
			ret[label] = string(s[l+1 : div])
			if max < div+start {
				max = div + start
			}
		}
	}
	ret[restname] = string(str[max+1:])
	return ret
}

func (p *NamedProcessCollector) scrape(ch chan<- prometheus.Metric) {
	permErrs, groups, err := p.Update(p.source.AllProcs())
	p.scrapePartialErrors += permErrs.Partial
	if err != nil {
		p.scrapeErrors++
		log.Printf("error reading procs: %v", err)
	} else {
		for gname, gcounts := range groups {
			// Don't forget to add any new label to slice below
			// groupname is default label.
			lmap := parse(gname, []string{"user", "pod"}, "groupname")
			ch <- desc["numprocs"].metric(float64(gcounts.Procs), lmap, "")
			ch <- desc["membytes"].metric(float64(gcounts.Memory.ResidentBytes), lmap, "resident")
			ch <- desc["membytes"].metric(float64(gcounts.Memory.VirtualBytes), lmap, "virtual")
			ch <- desc["membytes"].metric(float64(gcounts.Memory.VmSwapBytes), lmap, "swapped")
			ch <- desc["startTime"].metric(float64(gcounts.OldestStartTime.Unix()), lmap, "")
			ch <- desc["openFDs"].metric(float64(gcounts.OpenFDs), lmap, "")
			ch <- desc["worstFDRatio"].metric(float64(gcounts.WorstFDratio), lmap, "")
			ch <- desc["cpuSecs"].metric(gcounts.CPUUserTime, lmap, "user")
			ch <- desc["cpuSecs"].metric(gcounts.CPUSystemTime, lmap, "system")
			ch <- desc["readBytes"].metric(float64(gcounts.ReadBytes), lmap, "")
			ch <- desc["writeBytes"].metric(float64(gcounts.WriteBytes), lmap, "")
			ch <- desc["majorPageFaults"].metric(float64(gcounts.MajorPageFaults), lmap, "")
			ch <- desc["minorPageFaults"].metric(float64(gcounts.MinorPageFaults), lmap, "")
			ch <- desc["contextSwitches"].metric(float64(gcounts.CtxSwitchVoluntary), lmap, "voluntary")
			ch <- desc["contextSwitches"].metric(float64(gcounts.CtxSwitchNonvoluntary), lmap, "nonvoluntary")
			ch <- desc["numThreads"].metric(float64(gcounts.NumThreads), lmap, "")
			ch <- desc["states"].metric(float64(gcounts.States.Running), lmap, "Running")
			ch <- desc["states"].metric(float64(gcounts.States.Sleeping), lmap, "Sleeping")
			ch <- desc["states"].metric(float64(gcounts.States.Waiting), lmap, "Waiting")
			ch <- desc["states"].metric(float64(gcounts.States.Zombie), lmap, "Zombie")
			ch <- desc["states"].metric(float64(gcounts.States.Other), lmap, "Other")

			for wchan, count := range gcounts.Wchans {
				ch <- desc["threadWchan"].metric(float64(count), lmap, wchan)
			}

			if p.threads {
				for _, thr := range gcounts.Threads {
					lmap["threadname"] = thr.Name
					ch <- desc["threadCount"].metric(float64(thr.NumThreads), lmap, "")
					ch <- desc["threadCpuSecs"].metric(float64(thr.CPUUserTime), lmap, "user")
					ch <- desc["threadCpuSecs"].metric(float64(thr.CPUSystemTime), lmap, "system")
					ch <- desc["threadIoBytes"].metric(float64(thr.ReadBytes), lmap, "read")
					ch <- desc["threadIoBytes"].metric(float64(thr.WriteBytes), lmap, "write")
					ch <- desc["threadMajorPageFaults"].metric(float64(thr.MajorPageFaults), lmap, "")
					ch <- desc["threadMinorPageFaults"].metric(float64(thr.MinorPageFaults), lmap, "")
					ch <- desc["threadContextSwitches"].metric(float64(thr.CtxSwitchVoluntary), lmap, "voluntary")
					ch <- desc["threadContextSwitches"].metric(float64(thr.CtxSwitchNonvoluntary), lmap, "nonvoluntary")
				}
			}
		}
	}
	ch <- desc["scrapeErrors"].metric(float64(p.scrapeErrors), nil, "")
	ch <- desc["scrapeProcReadErrors"].metric(float64(p.scrapeProcReadErrors), nil, "")
	ch <- desc["scrapePartialErrors"].metric(float64(p.scrapePartialErrors), nil, "")
}
