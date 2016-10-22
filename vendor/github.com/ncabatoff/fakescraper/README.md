# fakescraper
Scrape Prometheus metrics from inside the app.  Handy for testing.

I use this when I'm writing Prometheus exporters and want to test them from the command line.
Before this I would start the daemon, run curl to fetch the metrics, then kill it.  Now I simply
do something like:

func main() {
	var (
		onceToStdout = flag.Bool("once-to-stdout", false,
			"Don't bind, instead just print the metrics once to stdout and exit")
	)

	flag.Parse()

	if *onceToStdout {
		fs := fakescraper.NewFakeScraper()
		fmt.Print(fs.Scrape())
		return
	}

	...

	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		log.Fatalf("Unable to setup HTTP server: %v", err)
	}
}
