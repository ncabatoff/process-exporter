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

	Filedesc struct {
		Open  int64 // -1 if we don't know
		Limit uint64
	}

	// Tracker tracks processes and records metrics.
	Tracker struct {
		// Tracked holds the processes are being monitored.  Processes
		// may be blacklisted such that they no longer get tracked by
		// setting their value in the Tracked map to nil.
		Tracked map[ProcId]*TrackedProc
		// ProcIds is a map from pid to ProcId.  This is a convenience
		// to allow finding the Tracked entry of a parent process.
		ProcIds map[int]ProcId
	}

	// TrackedProc accumulates metrics for a process, as well as
	// remembering an optional GroupName tag associated with it.
	TrackedProc struct {
		// lastUpdate is used internally during the update cycle to find which procs have exited
		lastUpdate time.Time
		// info is the most recently obtained info for this proc
		info ProcInfo
		// accum is the total CPU and IO accrued since we started tracking this proc
		accum Counts
		// lastaccum is the CPU and IO accrued in the last Update()
		lastaccum Counts
		// GroupName is an optional tag for this proc.
		GroupName string
	}

	trackedStats struct {
		aggregate, latest Counts
		Memory
		Filedesc
		start time.Time
	}

	collectErrors struct {
		// readErrors is incremented every time GetMetrics() returns an error.
		Read int
		// permissionErrors is incremented every time we're unable to collect
		// some metrics (e.g. I/O) for a tracked proc.
		Permission int
	}
)

func (tp *TrackedProc) GetName() string {
	return tp.info.Name
}

func (tp *TrackedProc) GetCmdLine() []string {
	return tp.info.Cmdline
}

func (tp *TrackedProc) GetStats() trackedStats {
	mem := Memory{Resident: tp.info.ResidentBytes, Virtual: tp.info.VirtualBytes}
	fd := Filedesc{Open: tp.info.OpenFDs, Limit: tp.info.MaxFDs}
	return trackedStats{
		aggregate: tp.accum,
		latest:    tp.lastaccum,
		Memory:    mem,
		Filedesc:  fd,
		start:     tp.info.StartTime,
	}
}

func NewTracker() *Tracker {
	return &Tracker{Tracked: make(map[ProcId]*TrackedProc), ProcIds: make(map[int]ProcId)}
}

func (t *Tracker) Track(groupName string, idinfo ProcIdInfo) {
	info := ProcInfo{idinfo.ProcStatic, idinfo.ProcMetrics}
	t.Tracked[idinfo.ProcId] = &TrackedProc{GroupName: groupName, info: info}
}

func (t *Tracker) Ignore(id ProcId) {
	t.Tracked[id] = nil
}

// Scan procs and update metrics for those which are tracked.  Processes that have gone
// away get removed from the Tracked map.  New processes are returned, along with the count
// of permission errors.
func (t *Tracker) Update(procs ProcIter) ([]ProcIdInfo, collectErrors, error) {
	now := time.Now()
	var newProcs []ProcIdInfo
	var colErrs collectErrors

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
			// This usually happens due to the proc having exited, i.e.
			// we lost the race.  We don't count that as an error.
			if err != ErrProcNotExist {
				colErrs.Read++
			}
			continue
		}

		if known {
			// newcounts: resource consumption since last cycle
			newcounts := Counts{Cpu: metrics.CpuTime - last.info.CpuTime}
			// Counts are also used in Grouper, to track aggregate usage
			// across groups.  It would be nice to expose that we weren't
			// able to get some metrics (e.g. due to permissions) but not nice
			// enough to warrant an extra per-group metric.  Instead we just
			// report 0 for proc metrics we can't get and increment the global
			// permissionErrors counter.
			if metrics.ReadBytes == -1 {
				colErrs.Permission++
			} else {
				newcounts.ReadBytes = uint64(metrics.ReadBytes - last.info.ReadBytes)
				newcounts.WriteBytes = uint64(metrics.WriteBytes - last.info.WriteBytes)
			}
			last.accum = Counts{
				Cpu:        last.accum.Cpu + newcounts.Cpu,
				ReadBytes:  last.accum.ReadBytes + newcounts.ReadBytes,
				WriteBytes: last.accum.WriteBytes + newcounts.WriteBytes,
			}

			last.info.ProcMetrics = metrics
			last.lastUpdate = now

			last.lastaccum = newcounts
		} else {
			static, err := procs.GetStatic()
			if err != nil {
				continue
			}
			newProcs = append(newProcs, ProcIdInfo{procId, static, metrics})

			// Is this a new process with the same pid as one we already know?
			if oldProcId, ok := t.ProcIds[procId.Pid]; ok {
				// Delete it from known, otherwise the cleanup below will remove the
				// ProcIds entry we're about to create
				delete(t.Tracked, oldProcId)
			}
			t.ProcIds[procId.Pid] = procId
		}

	}
	err := procs.Close()
	if err != nil {
		return nil, colErrs, fmt.Errorf("Error reading procs: %v", err)
	}

	// Rather than allocating a new map each time to detect procs that have
	// disappeared, we bump the last update time on those that are still
	// present.  Then as a second pass we traverse the map looking for
	// stale procs and removing them.
	for procId, pinfo := range t.Tracked {
		if pinfo == nil {
			// TODO is this a bug? we're not tracking the proc so we don't see it go away so ProcIds
			// and Tracked are leaking?
			continue
		}
		if pinfo.lastUpdate != now {
			delete(t.Tracked, procId)
			delete(t.ProcIds, procId.Pid)
		}
	}

	return newProcs, colErrs, nil
}
