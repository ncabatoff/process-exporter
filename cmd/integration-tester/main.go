package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// You wouldn't think our child could start before us, but I have observed it; maybe due to rounding?
var start = time.Now().Unix() - 1

func main() {
	var (
		flagProcessExporter = flag.String("process-exporter", "./process-exporter", "path to process-exporter")
		flagLoadGenerator   = flag.String("load-generator", "./load-generator", "path to load-generator")
		flagAttempts        = flag.Int("attempts", 3, "try this many times before returning failure")
		flagWriteSizeBytes  = flag.Int("write-size-bytes", 1024*1024, "how many bytes to write each cycle")
	)
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmdlg := exec.CommandContext(ctx, *flagLoadGenerator, "-write-size-bytes", strconv.Itoa(*flagWriteSizeBytes))
	var buf = &bytes.Buffer{}
	cmdlg.Stdout = buf
	err := cmdlg.Start()
	if err != nil {
		log.Fatalf("Error launching load generator %q: %v", *flagLoadGenerator, err)
	}
	for !strings.HasPrefix(buf.String(), "ready") {
		time.Sleep(time.Second)
	}

	success := false
	for i := 0; i < *flagAttempts; i++ {
		comm := filepath.Base(*flagLoadGenerator)
		cmdpe := exec.CommandContext(ctx, *flagProcessExporter, "-once-to-stdout-delay", "20s",
			"-procnames", comm, "-threads=true")
		out, err := cmdpe.Output()
		if err != nil {
			log.Fatalf("Error launching process-exporter %q: %v", *flagProcessExporter, err)
		}
		log.Println(string(out))

		results := getResults(comm, string(out))
		if verify(results) {
			success = true
			break
		}
		log.Printf("try %d/%d failed", i+1, *flagAttempts)
	}

	cancel()
	cmdlg.Wait()

	if !success {
		os.Exit(1)
	}
}

type result struct {
	name   string
	labels map[string]string
	value  float64
}

func getResults(group string, out string) map[string][]result {
	results := make(map[string][]result)

	skiplabel := fmt.Sprintf(`groupname="%s"`, group)
	lines := bufio.NewScanner(strings.NewReader(out))
	lines.Split(bufio.ScanLines)
	for lines.Scan() {
		line := lines.Text()
		metric, value := "", 0.0
		_, err := fmt.Sscanf(line, "namedprocess_namegroup_%s %f", &metric, &value)
		if err != nil {
			continue
		}

		pos := strings.IndexByte(metric, '{')
		if pos == -1 {
			log.Fatalf("cannot parse metric %q, no open curly found", metric)
		}

		name, labelstr := metric[:pos], metric[pos+1:]
		labelstr = labelstr[:len(labelstr)-1]
		labels := make(map[string]string)
		for _, kv := range strings.Split(labelstr, ",") {
			if kv != skiplabel {
				pieces := strings.SplitN(kv, "=", 2)
				labelname, labelvalue := pieces[0], pieces[1][1:len(pieces[1])-1]
				labels[labelname] = labelvalue
			}
		}

		results[name] = append(results[name], result{name, labels, value})
	}
	return results
}

func verify(results map[string][]result) bool {
	success := true

	assertExact := func(name string, got, want float64) {
		if got != want {
			success = false
			log.Printf("expected %s to be %f, got %f", name, want, got)
		}
	}

	assertGreaterOrEqual := func(name string, got, want float64) {
		if got < want {
			success = false
			log.Printf("expected %s to have at least %f, got %f", name, want, got)
		}
	}

	assertExact("num_procs", results["num_procs"][0].value, 1)

	// Four locked threads plus go runtime means more than 7, but we'll say 7 to play it safe.
	assertGreaterOrEqual("num_threads", results["num_threads"][0].value, 7)

	// Our child must have started later than us.
	assertGreaterOrEqual("oldest_start_time_seconds",
		results["oldest_start_time_seconds"][0].value, float64(start))

	for _, result := range results["states"] {
		switch state := result.labels["state"]; state {
		case "Other", "Zombie":
			assertExact("state "+state, result.value, 0)
		case "Running":
			assertGreaterOrEqual("state "+state, result.value, 2)
		case "Waiting":
			assertGreaterOrEqual("state "+state, result.value, 0)
		case "Sleeping":
			assertGreaterOrEqual("state "+state, result.value, 4)
		}
	}

	for _, result := range results["thread_count"] {
		switch tname := result.labels["threadname"]; tname {
		case "blocking", "sysbusy", "userbusy", "waiting":
			assertExact("thread_count "+tname, result.value, 1)
		case "main":
			assertGreaterOrEqual("thread_count "+tname, result.value, 3)
		}
	}

	for _, result := range results["thread_cpu_seconds_total"] {
		if result.labels["mode"] == "system" {
			switch tname := result.labels["threadname"]; tname {
			case "sysbusy", "blocking":
				assertGreaterOrEqual("thread_cpu_seconds_total system "+tname, result.value, 0.00001)
			default:
				assertGreaterOrEqual("thread_cpu_seconds_total system "+tname, result.value, 0)
			}
		} else if result.labels["mode"] == "user" {
			switch tname := result.labels["threadname"]; tname {
			case "userbusy":
				assertGreaterOrEqual("thread_cpu_seconds_total user "+tname, result.value, 0.00001)
			default:
				assertGreaterOrEqual("thread_cpu_seconds_total user "+tname, result.value, 0)
			}
		}
	}

	for _, result := range results["thread_io_bytes_total"] {
		tname, iomode := result.labels["threadname"], result.labels["iomode"]
		if iomode == "read" {
			continue
		}
		rname := fmt.Sprintf("%s %s %s", "thread_io_bytes_total", iomode, tname)

		switch tname {
		case "blocking", "sysbusy":
			assertGreaterOrEqual(rname, result.value, 0.00001)
		default:
			assertExact(rname, result.value, 0)
		}
	}

	otherwchan := 0.0
	for _, result := range results["threads_wchan"] {
		switch wchan := result.labels["wchan"]; wchan {
		case "poll_schedule_timeout":
			assertGreaterOrEqual(wchan, result.value, 1)
		case "futex_wait_queue_me":
			assertGreaterOrEqual(wchan, result.value, 4)
		default:
			// The specific wchan involved for the blocking thread varies by filesystem.
			otherwchan++
		}
	}
	// assertGreaterOrEqual("other wchan", otherwchan, 1)

	return success
}
