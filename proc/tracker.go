package proc

import (
	"fmt"
	"time"

	common "github.com/ncabatoff/process-exporter"
)

type (
	Counts struct {
		CpuUser         float64
		CpuSystem       float64
		ReadBytes       uint64
		WriteBytes      uint64
		MajorPageFaults uint64
		MinorPageFaults uint64
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
		// procIds is a map from pid to ProcId.  This is a convenience
		// to allow finding the Tracked entry of a parent process.
		procIds       map[int]ProcId
		trackChildren bool
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
		start      time.Time
		numThreads uint64
	}

	collectErrors struct {
		// readErrors is incremented every time GetMetrics() returns an error.
		Read int
		// permissionErrors is incremented every time we're unable to collect
		// some metrics (e.g. I/O) for a tracked proc.
		Permission int
	}
)

func (c *Counts) Add(c2 Counts) {
	c.CpuUser += c2.CpuUser
	c.CpuSystem += c2.CpuSystem
	c.ReadBytes += c2.ReadBytes
	c.WriteBytes += c2.WriteBytes
	c.MajorPageFaults += c2.MajorPageFaults
	c.MinorPageFaults += c2.MinorPageFaults
}

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
		aggregate:  tp.accum,
		latest:     tp.lastaccum,
		Memory:     mem,
		Filedesc:   fd,
		start:      tp.info.StartTime,
		numThreads: tp.info.NumThreads,
	}
}

func NewTracker(trackChildren bool) *Tracker {
	return &Tracker{
		Tracked:       make(map[ProcId]*TrackedProc),
		procIds:       make(map[int]ProcId),
		trackChildren: trackChildren,
	}
}

func (t *Tracker) Track(groupName string, idinfo ProcIdInfo) {
	info := ProcInfo{idinfo.ProcStatic, idinfo.ProcMetrics}
	t.Tracked[idinfo.ProcId] = &TrackedProc{GroupName: groupName, info: info}
}

func (t *Tracker) Ignore(id ProcId) {
	t.Tracked[id] = nil
}

func updateProc(metrics ProcMetrics, tproc *TrackedProc, updateTime time.Time, cerrs *collectErrors) {
	// newcounts: resource consumption since last cycle
	newcounts := Counts{
		CpuUser:         metrics.CpuUserTime - tproc.info.CpuUserTime,
		CpuSystem:       metrics.CpuSystemTime - tproc.info.CpuSystemTime,
		MajorPageFaults: metrics.MajorPageFaults - tproc.info.MajorPageFaults,
		MinorPageFaults: metrics.MinorPageFaults - tproc.info.MinorPageFaults,
	}
	// Counts are also used in Grouper, to track aggregate usage
	// across groups.  It would be nice to expose that we weren't
	// able to get some metrics (e.g. due to permissions) but not nice
	// enough to warrant an extra per-group metric.  Instead we just
	// report 0 for proc metrics we can't get and increment the global
	// permissionErrors counter.
	if metrics.ReadBytes == -1 {
		cerrs.Permission++
	} else {
		newcounts.ReadBytes = uint64(metrics.ReadBytes - tproc.info.ReadBytes)
		newcounts.WriteBytes = uint64(metrics.WriteBytes - tproc.info.WriteBytes)
	}
	tproc.accum.Add(newcounts)
	tproc.info.ProcMetrics = metrics
	tproc.lastUpdate = updateTime
	tproc.lastaccum = newcounts
}

// handleProc updates the tracker if it's a known and not ignored proc.
// If it's neither known nor ignored, newProc will be non-nil.
// It is not an error if the process disappears while we are reading
// its info out of /proc, it just means nothing will be returned and
// the tracker will be unchanged.
func (t *Tracker) handleProc(proc Proc, updateTime time.Time) (*ProcIdInfo, collectErrors) {
	var cerrs collectErrors
	procId, err := proc.GetProcId()
	if err != nil {
		return nil, cerrs
	}

	// Do nothing if we're ignoring this proc.
	last, known := t.Tracked[procId]
	if known && last == nil {
		return nil, cerrs
	}

	metrics, err := proc.GetMetrics()
	if err != nil {
		// This usually happens due to the proc having exited, i.e.
		// we lost the race.  We don't count that as an error.
		// If GetMetrics returns
		if err != ErrProcNotExist {
			cerrs.Read++
		}
		return nil, cerrs
	}

	var newProc *ProcIdInfo
	if known {
		updateProc(metrics, last, updateTime, &cerrs)
	} else {
		static, err := proc.GetStatic()
		if err != nil {
			return nil, cerrs
		}
		newProc = &ProcIdInfo{procId, static, metrics}

		// Is this a new process with the same pid as one we already know?
		// Then delete it from the known map, otherwise the cleanup in Update()
		// will remove the ProcIds entry we're creating here.
		if oldProcId, ok := t.procIds[procId.Pid]; ok {
			delete(t.Tracked, oldProcId)
		}
		t.procIds[procId.Pid] = procId
	}
	return newProc, cerrs
}

// update scans procs and updates metrics for those which are tracked. Processes
// that have gone away get removed from the Tracked map. New processes are
// returned, along with the count of permission errors.
func (t *Tracker) update(procs ProcIter) ([]ProcIdInfo, collectErrors, error) {
	var newProcs []ProcIdInfo
	var colErrs collectErrors
	var now = time.Now()

	for procs.Next() {
		newProc, cerrs := t.handleProc(procs, now)
		if newProc != nil {
			newProcs = append(newProcs, *newProc)
		}
		colErrs.Read += cerrs.Read
		colErrs.Permission += cerrs.Permission
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
			delete(t.procIds, procId.Pid)
		}
	}

	return newProcs, colErrs, nil
}

// checkAncestry walks the process tree recursively towards the root,
// stopping at pid 1 or upon finding a parent that's already tracked
// or ignored.  If we find a tracked parent track this one too; if not,
// ignore this one.
func (t *Tracker) checkAncestry(idinfo ProcIdInfo, newprocs map[ProcId]ProcIdInfo) string {
	ppid := idinfo.ParentPid
	pProcId := t.procIds[ppid]
	if pProcId.Pid < 1 {
		// Reached root of process tree without finding a tracked parent.
		t.Ignore(idinfo.ProcId)
		return ""
	}

	// Is the parent already known to the tracker?
	if ptproc, ok := t.Tracked[pProcId]; ok {
		if ptproc != nil {
			// We've found a tracked parent.
			t.Track(ptproc.GroupName, idinfo)
			return ptproc.GroupName
		} else {
			// We've found an untracked parent.
			t.Ignore(idinfo.ProcId)
			return ""
		}
	}

	// Is the parent another new process?
	if pinfoid, ok := newprocs[pProcId]; ok {
		if name := t.checkAncestry(pinfoid, newprocs); name != "" {
			// We've found a tracked parent, which implies this entire lineage should be tracked.
			t.Track(name, idinfo)
			return name
		}
	}

	// Parent is dead, i.e. we never saw it, or there's no tracked proc in our ancestry.
	t.Ignore(idinfo.ProcId)
	return ""
}

// Update tracks any new procs that should be according to policy, and updates
// the metrics for already tracked procs.  Permission errors are returned as a
// count, and will not affect the error return value.
func (t *Tracker) Update(iter ProcIter, namer common.MatchNamer) (collectErrors, error) {
	newProcs, colErrs, err := t.update(iter)
	if err != nil {
		return colErrs, err
	}

	// Step 1: track any new proc that should be tracked based on its name and cmdline.
	untracked := make(map[ProcId]ProcIdInfo)
	for _, idinfo := range newProcs {
		nacl := common.NameAndCmdline{Name: idinfo.Name, Cmdline: idinfo.Cmdline}
		wanted, gname := namer.MatchAndName(nacl)
		if !wanted {
			untracked[idinfo.ProcId] = idinfo
			continue
		}

		t.Track(gname, idinfo)
	}

	// Step 2: track any untracked new proc that should be tracked because its parent is tracked.
	if !t.trackChildren {
		return colErrs, nil
	}

	for _, idinfo := range untracked {
		if _, ok := t.Tracked[idinfo.ProcId]; ok {
			// Already tracked or ignored in an earlier iteration
			continue
		}

		t.checkAncestry(idinfo, untracked)
	}
	return colErrs, nil
}
