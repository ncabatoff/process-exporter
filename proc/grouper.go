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

func NewGrouper(procnames []string, n Namer) *Grouper {
	g := Grouper{
		wantProcNames: make(map[string]struct{}),
		Namer:         n,
		GroupStats:    make(map[string]Counts),
		tracker:       NewTracker(),
	}
	for _, name := range procnames {
		g.wantProcNames[name] = struct{}{}
	}
	return &g
}

func (g *Grouper) Update() error {
	newProcs, err := g.tracker.Update(AllProcs())
	if err != nil {
		return err
	}

	// Step 1: track any new proc that should be tracked based on its name and cmdline.
	for _, idinfo := range newProcs {
		gname := g.Namer.Name(NameAndCmdline{idinfo.Name, idinfo.Cmdline})
		if _, ok := g.wantProcNames[gname]; !ok {
			continue
		}

		g.tracker.Track(gname, idinfo)
	}

	// Step 2: track any untracked new proc that should be tracked because its parent is tracked.
	for _, idinfo := range newProcs {
		ppid := idinfo.ParentPid
		pProcId := g.tracker.ProcIds[ppid]
		if tproc, ok := g.tracker.Tracked[pProcId]; ok {
			g.tracker.Track(tproc.GroupName, idinfo)
		} else if _, ok := g.tracker.Tracked[idinfo.ProcId]; !ok {
			g.tracker.Tracked[idinfo.ProcId] = nil
		}
	}
	return nil
}

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
