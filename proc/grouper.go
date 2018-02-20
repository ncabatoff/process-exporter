package proc

import (
	"time"

	common "github.com/ncabatoff/process-exporter"
)

type (
	// Grouper is the top-level interface to the process metrics.  All tracked
	// procs sharing the same group name are aggregated.
	Grouper struct {
		// groupAccum records the historical accumulation of a group so that
		// we can avoid ever decreasing the counts we return.
		groupAccum  map[GroupId]Counts
		tracker     *Tracker
		threadAccum map[GroupId]map[string]Threads
	}

	// GroupId is a compound-key to group metrics.
	GroupId struct {
		Account string
		Name    string
	}

	// GroupById maps GroupId to group metrics.
	GroupById map[GroupId]Group

	// Threads collects metrics for threads in a group sharing a thread name.
	Threads struct {
		Name       string
		NumThreads int
		Counts
	}

	// Group describes the metrics of a single group.
	Group struct {
		Counts
		States
		Procs int
		Memory
		OldestStartTime time.Time
		OpenFDs         uint64
		WorstFDratio    float64
		NumThreads      uint64
		Threads         []Threads
	}
)

// Returns true if x < y.  Test designers should ensure they always have
// a unique name/numthreads combination for each group.
func lessThreads(x, y Threads) bool {
	if x.Name > y.Name {
		return false
	}
	if x.Name < y.Name {
		return true
	}
	if x.NumThreads > y.NumThreads {
		return false
	}
	if x.NumThreads < y.NumThreads {
		return true
	}
	return lessCounts(x.Counts, y.Counts)
}

// NewGrouper creates a grouper.
func NewGrouper(namer common.MatchNamer, trackChildren, trackThreads bool) *Grouper {
	g := Grouper{
		groupAccum:  make(map[GroupId]Counts),
		threadAccum: make(map[GroupId]map[string]Threads),
		tracker:     NewTracker(namer, trackChildren, trackThreads),
	}
	return &g
}

func groupadd(grp Group, ts Update) Group {
	var zeroTime time.Time

	grp.Procs++
	grp.Memory.ResidentBytes += ts.Memory.ResidentBytes
	grp.Memory.VirtualBytes += ts.Memory.VirtualBytes
	if ts.Filedesc.Open != -1 {
		grp.OpenFDs += uint64(ts.Filedesc.Open)
	}
	openratio := float64(ts.Filedesc.Open) / float64(ts.Filedesc.Limit)
	if grp.WorstFDratio < openratio {
		grp.WorstFDratio = openratio
	}
	grp.NumThreads += ts.NumThreads
	grp.Counts.Add(ts.Latest)
	grp.States.Add(ts.States)
	if grp.OldestStartTime == zeroTime || ts.Start.Before(grp.OldestStartTime) {
		grp.OldestStartTime = ts.Start
	}

	return grp
}

// Update asks the tracker to report on each tracked process by name.
// These are aggregated by groupname, augmented by accumulated counts
// from the past, and returned.  Note that while the Tracker reports
// only what counts have changed since last cycle, Grouper.Update
// returns counts that never decrease.  Even once the last process
// with name X disappears, name X will still appear in the results
// with the same counts as before; of course, all non-count metrics
// will be zero.
func (g *Grouper) Update(iter Iter) (CollectErrors, GroupById, error) {
	cerrs, tracked, err := g.tracker.Update(iter)
	if err != nil {
		return cerrs, nil, err
	}
	return cerrs, g.groups(tracked), nil
}

// Translate the updates into a new GroupById and update internal history.
func (g *Grouper) groups(tracked []Update) GroupById {
	groups := make(GroupById)
	threadsByGroup := make(map[GroupId][]ThreadUpdate)

	for _, update := range tracked {
		groupId := GroupId{update.Account, update.GroupName}

		groups[groupId] = groupadd(groups[groupId], update)
		if update.Threads != nil {
			threadsByGroup[groupId] =
				append(threadsByGroup[groupId], update.Threads...)
		}
	}

	// Add any accumulated counts to what was just observed,
	// and update the accumulators.
	for groupId, group := range groups {
		if oldcounts, ok := g.groupAccum[groupId]; ok {
			group.Counts.Add(Delta(oldcounts))
		}
		g.groupAccum[groupId] = group.Counts
		group.Threads = g.threads(groupId, threadsByGroup[groupId])
		groups[groupId] = group
	}

	// Now add any groups that were observed in the past but aren't running now.
	for groupId, gcounts := range g.groupAccum {
		if _, ok := groups[groupId]; !ok {
			groups[groupId] = Group{Counts: gcounts}
		}
	}

	return groups
}

func (g *Grouper) threads(groupId GroupId, tracked []ThreadUpdate) []Threads {
	if len(tracked) == 0 {
		delete(g.threadAccum, groupId)
		return nil
	}

	ret := make([]Threads, 0, len(tracked))
	threads := make(map[string]Threads)

	// First aggregate the thread metrics by thread name.
	for _, nc := range tracked {
		curthr := threads[nc.ThreadName]
		curthr.NumThreads++
		curthr.Counts.Add(nc.Latest)
		curthr.Name = nc.ThreadName
		threads[nc.ThreadName] = curthr
	}

	// Add any accumulated counts to what was just observed,
	// and update the accumulators.
	if history := g.threadAccum[groupId]; history != nil {
		for tname := range threads {
			if oldcounts, ok := history[tname]; ok {
				counts := threads[tname]
				counts.Add(Delta(oldcounts.Counts))
				threads[tname] = counts
			}
		}
	}

	g.threadAccum[groupId] = threads

	for _, thr := range threads {
		ret = append(ret, thr)
	}
	return ret
}
