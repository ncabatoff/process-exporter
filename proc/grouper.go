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

	GroupCountMap map[string]Groupcounts

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
// the metrics for already tracked procs.  Permission errors are returned as a
// count, and will not affect the error return value.
func (g *Grouper) Update(iter ProcIter) (int, error) {
	newProcs, permErrs, err := g.tracker.Update(iter)
	if err != nil {
		return permErrs, err
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
		return permErrs, nil
	}

	for _, idinfo := range untracked {
		if _, ok := g.tracker.Tracked[idinfo.ProcId]; ok {
			// Already tracked or ignored
			continue
		}

		g.checkAncestry(idinfo, untracked)
	}
	return permErrs, nil
}

// groups returns the aggregate metrics for all groups tracked.  This reflects
// solely what's currently running.
func (g *Grouper) groups() GroupCountMap {
	gcounts := make(GroupCountMap)

	for _, tinfo := range g.tracker.Tracked {
		if tinfo == nil {
			continue
		}
		cur := gcounts[tinfo.GroupName]
		cur.Procs++
		_, counts, mem := tinfo.GetStats()
		cur.Memresident += mem.Resident
		cur.Memvirtual += mem.Virtual
		cur.Counts.Cpu += counts.Cpu
		cur.Counts.ReadBytes += counts.ReadBytes
		cur.Counts.WriteBytes += counts.WriteBytes
		gcounts[tinfo.GroupName] = cur
	}

	return gcounts
}

// Groups returns Groupcounts with Counts that never decrease in value from one
// call to the next.  Even if processes exit, their CPU and IO contributions up
// to that point are included in the results.  Even if no processes remain
// in a group it will still be included in the results.
func (g *Grouper) Groups() GroupCountMap {
	groups := g.groups()

	// First add any accumulated counts to what was just observed,
	// and update the accumulators.
	for gname, group := range groups {
		if oldcounts, ok := g.GroupStats[gname]; ok {
			group.Counts.Cpu += oldcounts.Cpu
			group.Counts.ReadBytes += oldcounts.ReadBytes
			group.Counts.WriteBytes += oldcounts.WriteBytes
		}
		g.GroupStats[gname] = group.Counts
		groups[gname] = group
	}

	// Now add any groups that were observed in the past but aren't running now.
	for gname, gcounts := range g.GroupStats {
		if _, ok := groups[gname]; !ok {
			groups[gname] = Groupcounts{Counts: gcounts}
		}
	}

	return groups
}
