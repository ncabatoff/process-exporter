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
	// DockerResolver ...
	DockerResolver struct {
		debug        bool
		pods         map[int]string
		lastloadtime time.Time
		template     string
	}
)

// Stringer interface
func (r *DockerResolver) String() string {
	return fmt.Sprintf("%+v", r.pods)
}

// NewDockerResolver ...
func NewDockerResolver(debug bool, template string) *DockerResolver {
	return &DockerResolver{
		debug:    debug,
		pods:     make(map[int]string),
		template: template,
	}
}

// Resolve implements Resolver
func (r *DockerResolver) Resolve(pa *common.ProcAttributes) {
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
	ppid := pa.Pid
	for ppid > 1 {
		ppid = pa.ProcTree[ppid]
		if val, ok := r.pods[ppid]; ok {
			(*pa).Pod = val
			return
		}
	}
}

func (r *DockerResolver) load() {
	t := time.Now()
	// reload list of docker processes no more often than each 2 seconds. Should be enough...
	if t.Sub(r.lastloadtime).Seconds() < 2 {
		return
	}
	r.lastloadtime = t
	out, err := exec.Command("bash", "-c", "docker ps -q | xargs docker inspect --format '{{.State.Pid}} "+r.template+"'").Output()
	if err != nil {
		if r.debug {
			log.Printf("Error executing `docker ps`: %s", err)
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		//fmt.Println(line)
		fld := strings.Fields(line)
		if len(fld) > 1 {
			i, err := strconv.Atoi(fld[0])
			if err == nil {
				r.pods[i] = strings.Join(fld[1:], " ")
			}
		}
	}
}
