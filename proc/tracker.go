package proc

import (
	"fmt"
	"time"

	common "github.com/ncabatoff/process-exporter"
)

type (
	// Tracker tracks processes and records metrics.
	Tracker struct {
		// namer determines what processes to track and names them
		namer common.MatchNamer
		// tracked holds the processes are being monitored.  Processes
		// may be blacklisted such that they no longer get tracked by
		// setting their value in the tracked map to nil.
		tracked map[ID]*trackedProc
		// procIds is a map from pid to ProcId.  This is a convenience
		// to allow finding the Tracked entry of a parent process.
		procIds map[int]ID
		// trackChildren makes Tracker track descendants of procs the
		// namer wanted tracked.
		trackChildren bool
	}

	// trackedProc accumulates metrics for a process, as well as
	// remembering an optional GroupName tag associated with it.
	trackedProc struct {
		// lastUpdate is used internally during the update cycle to find which procs have exited
		lastUpdate time.Time
		// static
		static  Static
		metrics Metrics
		// lastaccum is the increment to the counters seen in the last update.
		lastaccum Counts
		// groupName is the tag for this proc given by the namer.
		groupName string
	}

	// Update reports on the latest stats for a process.
	Update struct {
		GroupName string
		Latest    Counts
		Memory
		Filedesc
		Start      time.Time
		NumThreads uint64
	}

	// CollectErrors describes non-fatal errors found while collecting proc
	// metrics.
	CollectErrors struct {
		// Read is incremented every time GetMetrics() returns an error.
		// This means we failed to load even the basics for the process,
		// and not just because it disappeared on us.
		Read int
		// Partial is incremented every time we're unable to collect
		// some metrics (e.g. I/O) for a tracked proc, but we're still able
		// to get the basic stuff like cmdline and core stats.
		Partial int
	}
)

func (tp *trackedProc) getUpdate() Update {
	return Update{
		GroupName:  tp.groupName,
		Latest:     tp.lastaccum,
		Memory:     tp.metrics.Memory,
		Filedesc:   tp.metrics.Filedesc,
		Start:      tp.static.StartTime,
		NumThreads: tp.metrics.NumThreads,
	}
}

// NewTracker creates a Tracker.
func NewTracker(namer common.MatchNamer, trackChildren bool) *Tracker {
	return &Tracker{
		namer:         namer,
		tracked:       make(map[ID]*trackedProc),
		procIds:       make(map[int]ID),
		trackChildren: trackChildren,
	}
}

func (t *Tracker) track(groupName string, idinfo IDInfo) {
	t.tracked[idinfo.ID] = &trackedProc{
		groupName: groupName,
		static:    idinfo.Static,
		metrics:   idinfo.Metrics,
	}
}

func (t *Tracker) ignore(id ID) {
	t.tracked[id] = nil
}

func updateProc(metrics Metrics, tproc *trackedProc, updateTime time.Time, cerrs *CollectErrors) {
	// newcounts: resource consumption since last cycle
	newcounts := metrics.Counts
	newcounts.Sub(tproc.metrics.Counts)

	tproc.metrics = metrics
	tproc.lastUpdate = updateTime
	tproc.lastaccum = newcounts
}

// handleProc updates the tracker if it's a known and not ignored proc.
// If it's neither known nor ignored, newProc will be non-nil.
// It is not an error if the process disappears while we are reading
// its info out of /proc, it just means nothing will be returned and
// the tracker will be unchanged.
func (t *Tracker) handleProc(proc Proc, updateTime time.Time) (*IDInfo, CollectErrors) {
	var cerrs CollectErrors
	procID, err := proc.GetProcID()
	if err != nil {
		return nil, cerrs
	}

	// Do nothing if we're ignoring this proc.
	last, known := t.tracked[procID]
	if known && last == nil {
		return nil, cerrs
	}

	metrics, softerrors, err := proc.GetMetrics()
	if err != nil {
		// This usually happens due to the proc having exited, i.e.
		// we lost the race.  We don't count that as an error.
		if err != ErrProcNotExist {
			cerrs.Read++
		}
		return nil, cerrs
	}
	cerrs.Partial += softerrors

	var newProc *IDInfo
	if known {
		updateProc(metrics, last, updateTime, &cerrs)
	} else {
		static, err := proc.GetStatic()
		if err != nil {
			return nil, cerrs
		}
		newProc = &IDInfo{procID, static, metrics}

		// Is this a new process with the same pid as one we already know?
		// Then delete it from the known map, otherwise the cleanup in Update()
		// will remove the ProcIds entry we're creating here.
		if oldProcID, ok := t.procIds[procID.Pid]; ok {
			delete(t.tracked, oldProcID)
		}
		t.procIds[procID.Pid] = procID
	}
	return newProc, cerrs
}

// update scans procs and updates metrics for those which are tracked. Processes
// that have gone away get removed from the Tracked map. New processes are
// returned, along with the count of permission errors.
func (t *Tracker) update(procs Iter) ([]IDInfo, CollectErrors, error) {
	var newProcs []IDInfo
	var colErrs CollectErrors
	var now = time.Now()

	for procs.Next() {
		newProc, cerrs := t.handleProc(procs, now)
		if newProc != nil {
			newProcs = append(newProcs, *newProc)
		}
		colErrs.Read += cerrs.Read
		colErrs.Partial += cerrs.Partial
	}

	err := procs.Close()
	if err != nil {
		return nil, colErrs, fmt.Errorf("Error reading procs: %v", err)
	}

	// Rather than allocating a new map each time to detect procs that have
	// disappeared, we bump the last update time on those that are still
	// present.  Then as a second pass we traverse the map looking for
	// stale procs and removing them.
	for procID, pinfo := range t.tracked {
		if pinfo == nil {
			// TODO is this a bug? we're not tracking the proc so we don't see it go away so ProcIds
			// and Tracked are leaking?
			continue
		}
		if pinfo.lastUpdate != now {
			delete(t.tracked, procID)
			delete(t.procIds, procID.Pid)
		}
	}

	return newProcs, colErrs, nil
}

// checkAncestry walks the process tree recursively towards the root,
// stopping at pid 1 or upon finding a parent that's already tracked
// or ignored.  If we find a tracked parent track this one too; if not,
// ignore this one.
func (t *Tracker) checkAncestry(idinfo IDInfo, newprocs map[ID]IDInfo) string {
	ppid := idinfo.ParentPid
	pProcID := t.procIds[ppid]
	if pProcID.Pid < 1 {
		// Reached root of process tree without finding a tracked parent.
		t.ignore(idinfo.ID)
		return ""
	}

	// Is the parent already known to the tracker?
	if ptproc, ok := t.tracked[pProcID]; ok {
		if ptproc != nil {
			// We've found a tracked parent.
			t.track(ptproc.groupName, idinfo)
			return ptproc.groupName
		}
		// We've found an untracked parent.
		t.ignore(idinfo.ID)
		return ""
	}

	// Is the parent another new process?
	if pinfoid, ok := newprocs[pProcID]; ok {
		if name := t.checkAncestry(pinfoid, newprocs); name != "" {
			// We've found a tracked parent, which implies this entire lineage should be tracked.
			t.track(name, idinfo)
			return name
		}
	}

	// Parent is dead, i.e. we never saw it, or there's no tracked proc in our ancestry.
	t.ignore(idinfo.ID)
	return ""
}

// Update tracks any new procs that should be according to policy, and updates
// the metrics for already tracked procs.  Permission errors are returned as a
// count, and will not affect the error return value.
func (t *Tracker) Update(iter Iter) (CollectErrors, []Update, error) {
	newProcs, colErrs, err := t.update(iter)
	if err != nil {
		return colErrs, nil, err
	}

	// Step 1: track any new proc that should be tracked based on its name and cmdline.
	untracked := make(map[ID]IDInfo)
	for _, idinfo := range newProcs {
		nacl := common.NameAndCmdline{Name: idinfo.Name, Cmdline: idinfo.Cmdline}
		wanted, gname := t.namer.MatchAndName(nacl)
		if wanted {
			t.track(gname, idinfo)
		} else {
			untracked[idinfo.ID] = idinfo
		}
	}

	// Step 2: track any untracked new proc that should be tracked because its parent is tracked.
	if t.trackChildren {
		for _, idinfo := range untracked {
			if _, ok := t.tracked[idinfo.ID]; ok {
				// Already tracked or ignored in an earlier iteration
				continue
			}

			t.checkAncestry(idinfo, untracked)
		}
	}

	tp := []Update{}
	for _, tproc := range t.tracked {
		if tproc != nil {
			tp = append(tp, tproc.getUpdate())
		}
	}
	return colErrs, tp, nil
}
