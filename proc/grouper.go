package proc

import (
	"time"

	common "github.com/ncabatoff/process-exporter"
)

type (
	Grouper struct {
		namer common.MatchNamer
		// track how much was seen last time so we can report the delta
		GroupStats map[string]Counts
		tracker    *Tracker
	}

	GroupCountMap map[string]GroupCounts

	GroupCounts struct {
		Counts
		Procs           int
		Memresident     uint64
		Memvirtual      uint64
		OldestStartTime time.Time
		OpenFDs         uint64
		WorstFDratio    float64
		NumThreads      uint64
	}
)

func NewGrouper(trackChildren bool, namer common.MatchNamer) *Grouper {
	g := Grouper{
		namer:      namer,
		GroupStats: make(map[string]Counts),
		tracker:    NewTracker(trackChildren),
	}
	return &g
}

func (g *Grouper) Update(iter ProcIter) (collectErrors, error) {
	return g.tracker.Update(iter, g.namer)
}

// curgroups returns the aggregate metrics for all curgroups tracked.  This reflects
// solely what's currently running.
func (g *Grouper) curgroups() GroupCountMap {
	gcounts := make(GroupCountMap)

	var zeroTime time.Time
	for _, tinfo := range g.tracker.Tracked {
		if tinfo == nil {
			continue
		}
		cur := gcounts[tinfo.GroupName]
		cur.Procs++
		tstats := tinfo.GetStats()
		cur.Memresident += tstats.Memory.Resident
		cur.Memvirtual += tstats.Memory.Virtual
		if tstats.Filedesc.Open != -1 {
			cur.OpenFDs += uint64(tstats.Filedesc.Open)
		}
		openratio := float64(tstats.Filedesc.Open) / float64(tstats.Filedesc.Limit)
		if cur.WorstFDratio < openratio {
			cur.WorstFDratio = openratio
		}
		cur.NumThreads += tstats.numThreads
		cur.Counts.Add(tstats.latest)
		if cur.OldestStartTime == zeroTime || tstats.start.Before(cur.OldestStartTime) {
			cur.OldestStartTime = tstats.start
		}
		gcounts[tinfo.GroupName] = cur
	}

	return gcounts
}

// Groups returns GroupCounts with Counts that never decrease in value from one
// call to the next.  Even if processes exit, their CPU and IO contributions up
// to that point are included in the results.  Even if no processes remain
// in a group it will still be included in the results.
func (g *Grouper) Groups() GroupCountMap {
	groups := g.curgroups()

	// First add any accumulated counts to what was just observed,
	// and update the accumulators.
	for gname, group := range groups {
		if oldcounts, ok := g.GroupStats[gname]; ok {
			group.Counts.Add(oldcounts)
		}
		g.GroupStats[gname] = group.Counts
		groups[gname] = group
	}

	// Now add any groups that were observed in the past but aren't running now.
	for gname, gcounts := range g.GroupStats {
		if _, ok := groups[gname]; !ok {
			groups[gname] = GroupCounts{Counts: gcounts}
		}
	}

	return groups
}
