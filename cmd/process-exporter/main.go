package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/ncabatoff/fakescraper"
	common "github.com/ncabatoff/process-exporter"
	"github.com/ncabatoff/process-exporter/collector"
	"github.com/ncabatoff/process-exporter/config"
	"github.com/prometheus/client_golang/prometheus"
	verCollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promslog"

	"github.com/prometheus/common/promslog/flag"
	promVersion "github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/exporter-toolkit/web/kingpinflag"
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
walking the process tree upwards. In other words, resource usage of
subprocesses is added to their parent's usage unless the subprocess identifies
as a different group name.

Command-line process selection (procnames/namemapping):

  Every process not in the procnames list is ignored.  Otherwise, all processes
  found are reported on as a group based on the process name they share.
  Here 'process name' refers to the value found in the second field of
  /proc/<pid>/stat, which is truncated at 15 chars.

  The -namemapping option allows assigning a group name based on a combination of
  the process name and command line. For example, using

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

type (
	prefixRegex struct {
		prefix string
		regex  *regexp.Regexp
	}

	nameMapperRegex struct {
		mapping map[string]*prefixRegex
	}
)

func (nmr *nameMapperRegex) String() string {
	return fmt.Sprintf("%+v", nmr.mapping)
}

// Create a nameMapperRegex based on a string given as the -namemapper argument.
func parseNameMapper(s string) (*nameMapperRegex, error) {
	mapper := make(map[string]*prefixRegex)
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
			matchName := name
			prefix := name + ":"

			if r, err := regexp.Compile(regexstr); err != nil {
				return nil, fmt.Errorf("error compiling regexp '%s': %v", regexstr, err)
			} else {
				mapper[matchName] = &prefixRegex{prefix: prefix, regex: r}
			}
		}
	}

	return &nameMapperRegex{mapper}, nil
}

func (nmr *nameMapperRegex) MatchAndName(nacl common.ProcAttributes) (bool, string) {
	if pregex, ok := nmr.mapping[nacl.Name]; ok {
		if pregex == nil {
			return true, nacl.Name
		}
		matches := pregex.regex.FindStringSubmatch(strings.Join(nacl.Cmdline, " "))
		if len(matches) > 1 {
			for _, matchstr := range matches[1:] {
				if matchstr != "" {
					return true, pregex.prefix + matchstr
				}
			}
		}
	}

	return false, ""
}

func init() {
	promVersion.Version = version
	prometheus.MustRegister(verCollector.NewCollector("process_exporter"))
}

func main() {
	var (
		webConfig         = kingpinflag.AddFlags(kingpin.CommandLine, ":9256")
		metricsPath       = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
		onceToStdoutDelay = kingpin.Flag("once-to-stdout-delay", "If set, will collect metrics once and print them to stdout after the given delay.").Default("0s").Duration()
		procNames         = kingpin.Flag("procnames", "comma-separated list of process names to monitor").String()
		procfsPath        = kingpin.Flag("procfs", "path to read proc data from").Default("/proc").String()
		nameMapping       = kingpin.Flag("namemapping", "comma-separated list, alternating process name and capturing regex to apply to cmdline").String()
		children          = kingpin.Flag("children", "if a proc is tracked, track with it any children that aren't part of their own group").Default("true").Bool()
		threads           = kingpin.Flag("threads", "report on per-threadname metrics as well").Default("true").Bool()
		smaps             = kingpin.Flag("gather-smaps", "gather metrics from smaps file, which contains proportional resident memory size").Bool()
		man               = kingpin.Flag("man", "print manual").Bool()
		configPath        = kingpin.Flag("config.path", "path to YAML config file").String()
		recheck           = kingpin.Flag("recheck", "recheck process names on each scrape").Bool()
		recheckTimeLimit  = kingpin.Flag("recheck-with-time-limit", "recheck processes only this much time after their start, but no longer.").Duration()
		removeEmptyGroups = kingpin.Flag("remove-empty-groups", "forget process groups with no processes").Bool()
	)

	promslogConfig := &promslog.Config{}

	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(promVersion.Print("process-exporter"))
	kingpin.HelpFlag.Short('h')

	kingpin.Parse()
	logger := promslog.New(promslogConfig)

	logger.Info("process-exporter", "version", promVersion.Info())
	logger.Info("build context", "build_context", promVersion.BuildContext())

	if *man {
		printManual()
		return
	}

	var matchnamer common.MatchNamer

	if *configPath != "" {
		if *nameMapping != "" || *procNames != "" {
			logger.Error("-config.path cannot be used with -namemapping or -procnames")
			os.Exit(1)
		}

		cfg, err := config.ReadFile(*configPath, logger)
		if err != nil {
			logger.Error("error reading config file", "config path", *configPath, "error", err.Error())
			os.Exit(1)
		}
		logger.Info("Reading metrics", "procfs path", *procfsPath, "config path", *configPath)
		matchnamer = cfg.MatchNamers
		logger.Debug("using config matchnamer", "config", cfg.MatchNamers)
	} else {
		namemapper, err := parseNameMapper(*nameMapping)
		if err != nil {
			logger.Error("Error parsing -namemapping argument", "arg", *nameMapping, "error", err.Error())
			os.Exit(1)
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

		logger.Info("Reading metrics", "procfs path", *procfsPath, "procnames", names)
		logger.Debug("using cmdline matchnamer", "cmdline", namemapper)
		matchnamer = namemapper
	}

	if *recheckTimeLimit != 0 {
		*recheck = true
	}

	pc, err := collector.NewProcessCollector(
		collector.ProcessCollectorOption{
			ProcFSPath:        *procfsPath,
			Children:          *children,
			Threads:           *threads,
			GatherSMaps:       *smaps,
			Namer:             matchnamer,
			Recheck:           *recheck,
			RecheckTimeLimit:  *recheckTimeLimit,
			RemoveEmptyGroups: *removeEmptyGroups,
		},
		logger,
	)
	if err != nil {
		logger.Error("Error initializing", "error", err.Error())
		os.Exit(1)
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

	http.Handle(*metricsPath, promhttp.Handler())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Named Process Exporter</title></head>
			<body>
			<h1>Named Process Exporter</h1>
			<p><a href="` + *metricsPath + `">Metrics</a></p>
			</body>
			</html>`))
	})
	server := &http.Server{}
	if err := web.ListenAndServe(server, webConfig, logger); err != nil {
		logger.Error("Failed to start the server", "error", err.Error())
		os.Exit(1)
	}
}
