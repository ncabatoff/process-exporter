package proc

import (
	"fmt"
	"github.com/prometheus/procfs"
	"time"
)

type (
	processId struct {
		// UNIX process id
		pid int
		// The time the process started after system boot, the value is expressed
		// in clock ticks.
		startTime uint64
	}

	Counts struct {
		Cpu        float64
		ReadBytes  uint64
		WriteBytes uint64
	}

	Memory struct {
		Resident uint64
		Virtual  uint64
	}

	Tracker struct {
		Procs map[processId]TrackedProc
		accum Counts
	}

	TrackedProc struct {
		// lastUpdate is used internally during the update cycle to find which procs have exited
		lastUpdate time.Time
		// lastvals is the procSum most recently obtained for this proc, i.e. its current metrics
		lastvals procSum
		// accum is the total CPU and IO accrued
		accum Counts
	}
)

func (tp *TrackedProc) GetName() string {
	return tp.lastvals.name
}

func (tp *TrackedProc) GetCmdLine() []string {
	return tp.lastvals.cmdline
}

func (tp *TrackedProc) GetStats() (Counts, Memory) {
	return tp.accum, Memory{Resident: tp.lastvals.memresident, Virtual: tp.lastvals.memvirtual}
}

func NewTracker() *Tracker {
	return &Tracker{make(map[processId]TrackedProc), Counts{}}
}

// Scan /proc and update oneself.  Rather than allocating a new map each time to detect procs
// that have disappeared, we bump the last update time on those that are still present.  Then
// as a second pass we traverse the map looking for stale procs and removing them.

func (t Tracker) Update() error {
	procs, err := procfs.AllProcs()
	if err != nil {
		return fmt.Errorf("Error reading procs: %v", err)
	}

	now := time.Now()
	for _, proc := range procs {
		psum, err := getProcSummary(proc)
		if err != nil {
			continue
		}
		xpid := processId{proc.PID, psum.startTime}

		var newaccum, lastaccum Counts
		if cur, ok := t.Procs[xpid]; ok {
			dcpu := psum.cpu - cur.lastvals.cpu
			drbytes := psum.readbytes - cur.lastvals.readbytes
			dwbytes := psum.writebytes - cur.lastvals.writebytes

			lastaccum = Counts{Cpu: dcpu, ReadBytes: drbytes, WriteBytes: dwbytes}
			newaccum = Counts{
				Cpu:        cur.accum.Cpu + lastaccum.Cpu,
				ReadBytes:  cur.accum.ReadBytes + lastaccum.ReadBytes,
				WriteBytes: cur.accum.WriteBytes + lastaccum.WriteBytes,
			}

			// log.Printf("%9d %20s %.1f %6d %6d", xpid.pid, psum.name, dCpu, drbytes, dwbytes)
		}
		t.Procs[xpid] = TrackedProc{lastUpdate: now, lastvals: psum, accum: newaccum}
	}

	for xpid, pinfo := range t.Procs {
		if pinfo.lastUpdate != now {
			delete(t.Procs, xpid)
		}
	}

	return nil
}
