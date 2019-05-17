package proc

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	common "github.com/opvizor/process-exporter"
)

type (
	// K8sResolver ...
	K8sResolver struct {
		debug        bool
		pods         map[int]string
		lastloadtime time.Time
		procfsPath   string
		defaultPod   string
	}
)

// Stringer interface
func (r *K8sResolver) String() string {
	return fmt.Sprintf("%+v", r.pods)
}

// NewK8sResolver ...
func NewK8sResolver(debug bool, procfsPath string, defaultPod string) *K8sResolver {
	out, err := exec.Command("bash", "-c", "curl --version >/dev/null && jq --version >/dev/null && echo 'OK'").CombinedOutput()
	outstr := strings.TrimSuffix(string(out), "\n")
	if err != nil || outstr != "OK" {
		log.Println("Error: curl or jq are not installed.\n\tDetails:", outstr,
			"\nPod names will not be resolved.")
		return nil
	}

	if os.Getenv("KUBE_TOKEN") == "" {
		b, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token") // just pass the file name
		if err != nil {
			log.Println("Error reading KUBE_TOKEN from /var/run/secrets/kubernetes.io/serviceaccount/token\n\tDetails:", err,
				"\nPod names will not be resolved.")
			return nil
		}
		os.Setenv("KUBE_TOKEN", string(b))
	}
	if debug {
		log.Println("KUBE_TOKEN:", os.Getenv("KUBE_TOKEN"))
	}
	if os.Getenv("KUBE_URL") == "" {
		os.Setenv("KUBE_URL", "https://"+os.Getenv("KUBERNETES_SERVICE_HOST")+":"+os.Getenv("KUBERNETES_PORT_443_TCP_PORT"))
	}
	if debug {
		log.Println("KUBE_URL:", os.Getenv("KUBE_URL"))
	}
	cmd := `curl -sSk -H "Authorization: Bearer $KUBE_TOKEN"  "$KUBE_URL/api/v1/pods" >/dev/null && echo 'OK'`
	out, err = exec.Command("bash", "-c", cmd).CombinedOutput()
	outstr = strings.TrimSuffix(string(out), "\n")
	if err != nil || outstr != "OK" {
		log.Println("Error: K8S environment variables KUBERNETES_SERVICE_HOST, KUBERNETES_PORT_443_TCP_PORT seems to be misconfigured.\n\tDetails:",
			outstr, "\nPod names will not be resolved.")
		return nil
	}

	if procfsPath == "" {
		procfsPath = "/proc"
	}

	cmd = `ls ` + procfsPath + `/*/cgroup >/dev/null ; echo $?`
	out, err = exec.Command("bash", "-c", cmd).CombinedOutput()
	outstr = strings.TrimSuffix(string(out), "\n")
	if err != nil || outstr != "0" {
		log.Println("Error: can't access host's /proc. Please check -procfs parameter.\n\tDetails:",
			outstr, "\nPod names will not be resolved.")
		return nil
	}

	return &K8sResolver{
		debug:      debug,
		pods:       make(map[int]string),
		procfsPath: procfsPath,
		defaultPod: defaultPod,
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
		return
	}
	(*pa).Pod = r.defaultPod
}

func (r *K8sResolver) load() {
	t := time.Now()
	// reload list of k8s pods no more often than each 2 seconds. Should be enough...
	if t.Sub(r.lastloadtime).Seconds() < 2 {
		return
	}
	r.lastloadtime = t
	// get pids with container names from cgroups
	c := strings.Count(r.procfsPath, "/")
	f := fmt.Sprintf("%d,%d,%d", c+2, c+6, c+7)
	cmd := `grep -r "1:name=.*/kubepods" ` + r.procfsPath + `/*/cgroup | cut -d '/' -f` + f + ` | sed  "s/\/pod/\//g" | sed "s/\// /g"`
	out, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		log.Println("Error accessing procfs: ", err)
		return
	}
	if r.debug {
		log.Println(string(out))
	}
	strpids := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	// get pods names and containers from k8s /api/v1/pods
	cmd = `curl -sSk  -H "Authorization: Bearer $KUBE_TOKEN" "$KUBE_URL/api/v1/pods" |jq -r '.items[] | "\(.metadata.name) \(.status.containerStatuses[]?.containerID)"'|sed -E "s/\w+:\/\///g"`
	out, err = exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		log.Println("Error receiving k8s pods: ", err)
		return
	}
	if r.debug {
		log.Println(string(out))
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
