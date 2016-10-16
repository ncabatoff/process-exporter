package proc

import (
	"fmt"
	"time"
)

type (
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
		Procs map[ProcId]TrackedProc
		accum Counts
	}

	TrackedProc struct {
		// lastUpdate is used internally during the update cycle to find which procs have exited
		lastUpdate time.Time
		// lastvals is the procSum most recently obtained for this proc, i.e. its current metrics
		lastvals ProcInfo
		// accum is the total CPU and IO accrued
		accum Counts
	}
)

func (tp *TrackedProc) GetName() string {
	return tp.lastvals.Name
}

func (tp *TrackedProc) GetCmdLine() []string {
	return tp.lastvals.Cmdline
}

func (tp *TrackedProc) GetStats() (Counts, Memory) {
	return tp.accum, Memory{Resident: tp.lastvals.ResidentBytes, Virtual: tp.lastvals.VirtualBytes}
}

func NewTracker() *Tracker {
	return &Tracker{make(map[ProcId]TrackedProc), Counts{}}
}

// Scan /proc and update oneself.  Rather than allocating a new map each time to detect procs
// that have disappeared, we bump the last update time on those that are still present.  Then
// as a second pass we traverse the map looking for stale procs and removing them.

func (t Tracker) Update() error {
	now := time.Now()
	allProcs := AllProcs()
	for allProcs.Next() {
		procId, err := allProcs.GetProcId()
		if err != nil {
			continue
		}

		metrics, err := allProcs.GetMetrics()
		if err != nil {
			continue
		}

		static, err := allProcs.GetStatic()
		if err != nil {
			continue
		}

		var newaccum, lastaccum Counts
		if cur, ok := t.Procs[procId]; ok {
			dcpu := metrics.CpuTime - cur.lastvals.CpuTime
			drbytes := metrics.ReadBytes - cur.lastvals.ReadBytes
			dwbytes := metrics.WriteBytes - cur.lastvals.WriteBytes

			lastaccum = Counts{Cpu: dcpu, ReadBytes: drbytes, WriteBytes: dwbytes}
			newaccum = Counts{
				Cpu:        cur.accum.Cpu + lastaccum.Cpu,
				ReadBytes:  cur.accum.ReadBytes + lastaccum.ReadBytes,
				WriteBytes: cur.accum.WriteBytes + lastaccum.WriteBytes,
			}

			// log.Printf("%9d %20s %.1f %6d %6d", xpid.pid, psum.name, dCpu, drbytes, dwbytes)
		}
		info := ProcInfo{static, metrics}
		t.Procs[procId] = TrackedProc{lastUpdate: now, lastvals: info, accum: newaccum}
	}
	err := allProcs.Close()
	if err != nil {
		return fmt.Errorf("Error reading procs: %v", err)
	}

	for procId, pinfo := range t.Procs {
		if pinfo.lastUpdate != now {
			delete(t.Procs, procId)
		}
	}

	return nil
}
