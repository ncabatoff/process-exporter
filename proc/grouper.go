package proc

type (
	NameAndCmdline struct {
		Name    string
		Cmdline []string
	}

	Namer interface {
		// Map returns the name to use for a given process
		Name(NameAndCmdline) string
	}

	Grouper struct {
		wantProcNames map[string]struct{}
		trackChildren bool
		Namer
		// track how much was seen last time so we can report the delta
		GroupStats map[string]Counts
		tracker    *Tracker
	}

	Groupcounts struct {
		Counts
		Procs       int
		Memresident uint64
		Memvirtual  uint64
	}
)

// NewGrouper creates a Grouper that tracks the listed procnames.  Namer is used
// to map proc names+cmdlines to a tag name.
func NewGrouper(procnames []string, trackChildren bool, n Namer) *Grouper {
	g := Grouper{
		wantProcNames: make(map[string]struct{}),
		trackChildren: trackChildren,
		Namer:         n,
		GroupStats:    make(map[string]Counts),
		tracker:       NewTracker(),
	}
	for _, name := range procnames {
		g.wantProcNames[name] = struct{}{}
	}
	return &g
}

func (g *Grouper) checkAncestry(idinfo ProcIdInfo, newprocs map[ProcId]ProcIdInfo) string {
	ppid := idinfo.ParentPid
	pProcId := g.tracker.ProcIds[ppid]
	if pProcId.Pid < 1 {
		// Reached root of process tree without finding a tracked parent.
		g.tracker.Ignore(idinfo.ProcId)
		return ""
	}

	// Is the parent already known to the tracker?
	if ptproc, ok := g.tracker.Tracked[pProcId]; ok {
		if ptproc != nil {
			// We've found a tracked parent.
			g.tracker.Track(ptproc.GroupName, idinfo)
			return ptproc.GroupName
		} else {
			// We've found an untracked parent.
			g.tracker.Ignore(idinfo.ProcId)
			return ""
		}
	}

	// Is the parent another new process?
	if pinfoid, ok := newprocs[pProcId]; ok {
		if name := g.checkAncestry(pinfoid, newprocs); name != "" {
			// We've found a tracked parent, which implies this entire lineage should be tracked.
			g.tracker.Track(name, idinfo)
			return name
		}
	}

	// Parent is dead, i.e. we never saw it, or there's no tracked proc in our ancestry.
	g.tracker.Ignore(idinfo.ProcId)
	return ""

}

// Update tracks any new procs that should be according to policy, and updates
// the metrics for already tracked procs.
func (g *Grouper) Update(iter ProcIter) error {
	newProcs, err := g.tracker.Update(iter)
	if err != nil {
		return err
	}

	// Step 1: track any new proc that should be tracked based on its name and cmdline.
	untracked := make(map[ProcId]ProcIdInfo)
	for _, idinfo := range newProcs {
		gname := g.Namer.Name(NameAndCmdline{idinfo.Name, idinfo.Cmdline})
		if _, ok := g.wantProcNames[gname]; !ok {
			untracked[idinfo.ProcId] = idinfo
			continue
		}

		g.tracker.Track(gname, idinfo)
	}

	// Step 2: track any untracked new proc that should be tracked because its parent is tracked.
	if !g.trackChildren {
		return nil
	}

	for _, idinfo := range untracked {
		if _, ok := g.tracker.Tracked[idinfo.ProcId]; ok {
			// Already tracked or ignored
			continue
		}

		g.checkAncestry(idinfo, untracked)
	}
	return nil
}

// Groups returns the aggregate metrics for all groups tracked.
func (g *Grouper) Groups() map[string]Groupcounts {
	gcounts := make(map[string]Groupcounts)

	for _, tinfo := range g.tracker.Tracked {
		if tinfo == nil {
			continue
		}
		cur := gcounts[tinfo.GroupName]
		cur.Procs++
		counts, mem := tinfo.GetStats()
		cur.Memresident += mem.Resident
		cur.Memvirtual += mem.Virtual
		cur.Counts.Cpu += counts.Cpu
		cur.Counts.ReadBytes += counts.ReadBytes
		cur.Counts.WriteBytes += counts.WriteBytes
		gcounts[tinfo.GroupName] = cur
	}

	return gcounts
}
