package proc

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	common "github.com/ncabatoff/process-exporter"
)

type (
	// K8sResolver ...
	K8sResolver struct {
		debug        bool
		pods         map[int]string
		lastloadtime time.Time
		procfsPath   string
	}
)

// Stringer interface
func (r *K8sResolver) String() string {
	return fmt.Sprintf("%+v", r.pods)
}

// NewK8sResolver ...
func NewK8sResolver(debug bool, procfsPath string) *K8sResolver {
	out, err := exec.Command("bash", "-c", "curl --version >/dev/null && jq --version >/dev/null && echo 'OK'").CombinedOutput()
	outstr := strings.TrimSuffix(string(out), "\n")
	if err != nil || outstr != "OK" {
		log.Println("Error: curl or jq are not installed. Pod names will not be resolved. \n\tDetails:", outstr)
		return nil
	}

	cmd := `curl -sSk  -H "Authorization: Bearer $KUBE_TOKEN" "$KUBE_URL/api/v1/pods" >/dev/null && echo 'OK'`
	out, err = exec.Command("bash", "-c", cmd).CombinedOutput()
	outstr = strings.TrimSuffix(string(out), "\n")
	if err != nil || outstr != "OK" {
		log.Println("Error: K8S environment variables KUBE_TOKEN and KUBE_URL seems to be misconfigured. Pod names will not be resolved.\n\tDetails:",
			outstr)
		return nil
	}

	return &K8sResolver{
		debug:      debug,
		pods:       make(map[int]string),
		procfsPath: procfsPath,
	}
}

// Resolve implements Resolver
func (r *K8sResolver) Resolve(pa *common.ProcAttributes) {
	if r == nil {
		return
	}
	if r.debug {
		log.Printf("Resolving pid %d", pa.Pid)
	}
	if val, ok := r.pods[pa.Pid]; ok {
		(*pa).Pod = val
		return
	}
	r.load()
	if val, ok := r.pods[pa.Pid]; ok {
		(*pa).Pod = val
	}
}

func (r *K8sResolver) load() {
	t := time.Now()
	// reload list of k8s pods no more often than each 2 seconds. Should be enough...
	if t.Sub(r.lastloadtime).Seconds() < 2 {
		return
	}
	r.lastloadtime = t
	// get pids with container names from cgroups
	cmd := `grep -r "1:name=.*/kubepods" /proc/*/cgroup | cut -d '/' -f3,7,8 | sed  "s/\/pod/\//g" | sed "s/\// /g"`
	out, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		log.Println("Error accessing procfs: ", err)
		return
	}
	strpids := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	// get pods names and containers from k8s /api/v1/pods
	cmd = `curl -sSk  -H "Authorization: Bearer $KUBE_TOKEN" "$KUBE_URL/api/v1/pods" |jq -r '.items[] | "\(.metadata.name) \(.status.containerStatuses[]?.containerID)"'|sed -E "s/\w+:\/\///g"`
	out, err = exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		log.Println("Error receiving k8s pods: ", err)
		return
	}
	strpods := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	//parse output
	containers := make(map[int]string)
	for _, line := range strpids {
		fld := strings.Fields(line)
		if len(fld) < 2 {
			break
		}
		pid, err := strconv.Atoi(fld[0])
		if err != nil {
			break
		}
		if len(fld) > 2 {
			containers[pid] = fld[2]
		} else {
			containers[pid] = fld[1]
		}
	}
	podnames := make(map[string]string)
	for _, line := range strpods {
		fld := strings.Fields(line)
		if len(fld) < 2 {
			break
		}
		podnames[fld[1]] = fld[0]
	}
	for k, v := range containers {
		podname, ok := podnames[v]
		if ok {
			r.pods[k] = podname
		}
	}
}
