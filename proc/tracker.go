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

	FilterFunc func(ProcStatic) bool

	// Tracker observes processes.  When prompted it scans /proc and updates its records.
	// Processes may be blacklisted such that they no longer get tracked by setting their
	// value in the Tracked map to nil.
	Tracker struct {
		Tracked map[ProcId]*TrackedProc
		Filter  FilterFunc
	}

	TrackedProc struct {
		// lastUpdate is used internally during the update cycle to find which procs have exited
		lastUpdate time.Time
		// lastvals is the procSum most recently obtained for this proc, i.e. its current metrics
		info ProcInfo
		// accum is the total CPU and IO accrued
		accum Counts
	}
)

func (tp *TrackedProc) GetName() string {
	return tp.info.Name
}

func (tp *TrackedProc) GetCmdLine() []string {
	return tp.info.Cmdline
}

func (tp *TrackedProc) GetStats() (Counts, Memory) {
	return tp.accum, Memory{Resident: tp.info.ResidentBytes, Virtual: tp.info.VirtualBytes}
}

func NewTracker() *Tracker {
	return &Tracker{Tracked: make(map[ProcId]*TrackedProc)}
}

// Scan procs and update oneself.  Rather than allocating a new map each time to detect procs
// that have disappeared, we bump the last update time on those that are still present.  Then
// as a second pass we traverse the map looking for stale procs and removing them.

func (t Tracker) Update(procs Procs) error {
	now := time.Now()
	for procs.Next() {
		procId, err := procs.GetProcId()
		if err != nil {
			continue
		}

		last, known := t.Tracked[procId]

		// Are we ignoring this proc?
		if known && last == nil {
			continue
		}

		metrics, err := procs.GetMetrics()
		if err != nil {
			continue
		}

		if known {
			var newaccum, lastaccum Counts
			dcpu := metrics.CpuTime - last.info.CpuTime
			drbytes := metrics.ReadBytes - last.info.ReadBytes
			dwbytes := metrics.WriteBytes - last.info.WriteBytes

			lastaccum = Counts{Cpu: dcpu, ReadBytes: drbytes, WriteBytes: dwbytes}
			newaccum = Counts{
				Cpu:        last.accum.Cpu + lastaccum.Cpu,
				ReadBytes:  last.accum.ReadBytes + lastaccum.ReadBytes,
				WriteBytes: last.accum.WriteBytes + lastaccum.WriteBytes,
			}

			info := ProcInfo{ProcStatic: last.info.ProcStatic, ProcMetrics: metrics}
			*(t.Tracked[procId]) = TrackedProc{lastUpdate: now, info: info, accum: newaccum}
		} else {
			static, err := procs.GetStatic()
			if err != nil {
				continue
			}
			info := ProcInfo{ProcStatic: static, ProcMetrics: metrics}
			t.Tracked[procId] = &TrackedProc{lastUpdate: now, info: info}
		}

	}
	err := procs.Close()
	if err != nil {
		return fmt.Errorf("Error reading procs: %v", err)
	}

	for procId, pinfo := range t.Tracked {
		if pinfo.lastUpdate != now {
			delete(t.Tracked, procId)
		}
	}

	return nil
}
