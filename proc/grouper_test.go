package proc

import (
	"time"

	"github.com/kylelemons/godebug/pretty"
	common "github.com/ncabatoff/process-exporter"
	. "gopkg.in/check.v1"
)

type namer map[string]struct{}

func newNamer(names ...string) namer {
	nr := make(namer, len(names))
	for _, name := range names {
		nr[name] = struct{}{}
	}
	return nr
}

func (n namer) MatchAndName(nacl common.NameAndCmdline) (bool, string) {
	if _, ok := n[nacl.Name]; ok {
		return true, nacl.Name
	}
	return false, ""
}

func newProc(pid int, name string, m ProcMetrics) ProcIdInfo {
	pis := newProcIdStatic(pid, 0, 0, name, nil)
	return ProcIdInfo{
		ProcId:      pis.ProcId,
		ProcStatic:  pis.ProcStatic,
		ProcMetrics: m,
	}
}

type grouptest struct {
	grouper *Grouper
	procs   ProcIter
	want    GroupCountMap
}

func (gt grouptest) run(c *C) {
	_, err := gt.grouper.Update(gt.procs)
	c.Assert(err, IsNil)

	got := gt.grouper.curgroups()
	c.Check(got, DeepEquals, gt.want, Commentf("diff %s", pretty.Compare(got, gt.want)))
}

func run(c *C, gr *Grouper, procs ProcIter) GroupCountMap {
	_, err := gr.Update(procs)
	c.Assert(err, IsNil)

	return gr.curgroups()
}

// TestGrouperBasic tests core Update/curgroups functionality on single-proc
// groups: the grouper adds to counts and updates the other tracked metrics like
// Memory.
func (s MySuite) TestGrouperBasic(c *C) {
	pid1, pid2 := 1, 2
	gr := NewGrouper(false, newNamer("g1", "g2"))
	m1t1 := ProcMetrics{Counts{1, 2, 3, 4, 5, 6}, Memory{7, 8}, Filedesc{4, 400}, 2}
	m2t1 := ProcMetrics{Counts{2, 3, 4, 5, 6, 7}, Memory{8, 9}, Filedesc{40, 400}, 3}
	procs1 := procInfoIter(newProc(pid1, "g1", m1t1), newProc(pid2, "g2", m2t1))
	want1 := GroupCountMap{
		"g1": GroupCounts{Counts{}, 1, Memory{7, 8}, time.Time{}, 4, 0.01, 2},
		"g2": GroupCounts{Counts{}, 1, Memory{8, 9}, time.Time{}, 40, 0.1, 3},
	}
	got1 := run(c, gr, procs1)
	c.Check(got1, DeepEquals, want1, Commentf("diff %s", pretty.Compare(got1, want1)))

	m1t2 := ProcMetrics{Counts{2, 3, 4, 5, 6, 7}, Memory{6, 7}, Filedesc{100, 400}, 4}
	m2t2 := ProcMetrics{Counts{4, 5, 6, 7, 8, 9}, Memory{9, 8}, Filedesc{400, 400}, 2}
	procs2 := procInfoIter(newProc(pid1, "g1", m1t2), newProc(pid2, "g2", m2t2))
	want2 := GroupCountMap{
		"g1": GroupCounts{Counts{1, 1, 1, 1, 1, 1}, 1, Memory{6, 7}, time.Time{}, 100, 0.25, 4},
		"g2": GroupCounts{Counts{2, 2, 2, 2, 2, 2}, 1, Memory{9, 8}, time.Time{}, 400, 1, 2},
	}
	got2 := run(c, gr, procs2)
	c.Check(got2, DeepEquals, want2, Commentf("diff %s", pretty.Compare(got2, want2)))
}

// TestGrouperProcJoinsGroup tests the appearance of a new process in a group,
// and that all procs metrics contribute to a group.
func (s MySuite) TestGrouperProcJoinsGroup(c *C) {
	pid1, pid2 := 1, 2
	gr := NewGrouper(false, newNamer("g1"))
	m1t1 := ProcMetrics{Counts{1, 2, 3, 4, 5, 6}, Memory{3, 4}, Filedesc{4, 400}, 2}
	procs1 := procInfoIter(newProc(pid1, "g1", m1t1))

	g1t1 := GroupCounts{Counts{}, 1, Memory{3, 4}, time.Time{}, 4, 0.01, 2}
	grouptest{gr, procs1, GroupCountMap{"g1": g1t1}}.run(c)

	// The counts for pid2 won't be factored into the total yet because we only add
	// to counts starting with the second time we see a proc. Memory and FDs are
	// affected though.
	m1t2 := ProcMetrics{Counts{3, 4, 5, 6, 7, 8}, Memory{3, 4}, Filedesc{4, 400}, 2}
	m2t2 := ProcMetrics{Counts{1, 1, 1, 1, 1, 1}, Memory{1, 2}, Filedesc{40, 400}, 3}
	procs2 := procInfoIter(newProc(pid1, "g1", m1t2), newProc(pid2, "g1", m2t2))
	g1t2 := GroupCounts{Counts{2, 2, 2, 2, 2, 2}, 2, Memory{4, 6}, time.Time{}, 44, 0.1, 5}
	grouptest{gr, procs2, GroupCountMap{"g1": g1t2}}.run(c)

	// Make sure that both procs now contribute to the group as they get updated.
	m1t3 := ProcMetrics{Counts{4, 5, 6, 7, 8, 9}, Memory{1, 5}, Filedesc{4, 400}, 2}
	m2t3 := ProcMetrics{Counts{2, 2, 2, 2, 2, 2}, Memory{2, 4}, Filedesc{40, 400}, 3}
	procs3 := procInfoIter(newProc(pid1, "g1", m1t3), newProc(pid2, "g1", m2t3))
	g1t3 := GroupCounts{Counts{2, 2, 2, 2, 2, 2}, 2, Memory{3, 9}, time.Time{}, 44, 0.1, 5}
	grouptest{gr, procs3, GroupCountMap{"g1": g1t3}}.run(c)
}

/*
	   // Test that if a proc is tracked, we track its descendants, and if not as
	   // before it gets ignored.  We won't bother testing metric accumulation since
	   // that should be covered by TestGrouperBasic.
	   func (s MySuite) TestGrouperParents(c *C) {
	   	newProc := func(pid, ppid int, name string) ProcIdInfo {
	   		pis := newProcIdStatic(pid, ppid, 0, name, nil)
	   		return ProcIdInfo{
	   			ProcId:      pis.ProcId,
	   			ProcStatic:  pis.ProcStatic,
	   			ProcMetrics: ProcMetrics{NumThreads: 1},
	   		}
	   	}
	   	gr := NewGrouper(true, newNamer("g1", "g2"))
	   	p1 := newProc(1, 0, "g1")
	   	p2 := newProc(2, 0, "g2")
	   	p3 := newProc(3, 0, "g3")

	   	_, err := gr.Update(procInfoIter(p1, p2, p3))
	   	c.Assert(err, IsNil)

	   	got1 := gr.curgroups()
	   	want1 := GroupCountMap{
	   		"g1": GroupCounts{Counts{}, 1, 0, 0, time.Time{}, 0, 0, 1},
	   		"g2": GroupCounts{Counts{}, 1, 0, 0, time.Time{}, 0, 0, 1},
	   	}
	   	c.Check(got1, DeepEquals, want1, Commentf("diff %s", pretty.Compare(got1, want1)))

	   	// Now we'll give each of the procs a child and test that the count of procs
	   	// in each group is incremented.

	   	p4 := newProc(4, p1.Pid, "")
	   	p5 := newProc(5, p2.Pid, "")
	   	p6 := newProc(6, p3.Pid, "")

	   	_, err = gr.Update(procInfoIter(p1, p2, p3, p4, p5, p6))
	   	c.Assert(err, IsNil)

	   	got2 := gr.curgroups()
	   	want2 := GroupCountMap{
	   		"g1": GroupCounts{Counts{}, 2, 0, 0, time.Time{}, 0, 0, 2},
	   		"g2": GroupCounts{Counts{}, 2, 0, 0, time.Time{}, 0, 0, 2},
	   	}
	   	c.Check(got2, DeepEquals, want2, Commentf("diff %s", pretty.Compare(got2, want2)))

	   	// Now we'll let p4 die, and give p5 a child and grandchild and great-grandchild.

	   	p7 := newProc(7, p5.Pid, "")
	   	p8 := newProc(8, p7.Pid, "")
	   	p9 := newProc(9, p8.Pid, "")

	   	_, err = gr.Update(procInfoIter(p1, p2, p3, p5, p6, p7, p8, p9))
	   	c.Assert(err, IsNil)

	   	got3 := gr.curgroups()
	   	want3 := GroupCountMap{
	   		"g1": GroupCounts{Counts{}, 1, 0, 0, time.Time{}, 0, 0, 1},
	   		"g2": GroupCounts{Counts{}, 5, 0, 0, time.Time{}, 0, 0, 5},
	   	}
	   	c.Check(got3, DeepEquals, want3, Commentf("diff %s", pretty.Compare(got3, want3)))
	   }

	   // Test that Groups() reports on new CPU/IO activity, even if some processes in the
	   // group have gone away.
	   func (s MySuite) TestGrouperGroup(c *C) {
	   	newProc := func(pid int, name string, m ProcMetrics) ProcIdInfo {
	   		pis := newProcIdStatic(pid, 0, 0, name, nil)
	   		return ProcIdInfo{
	   			ProcId:      pis.ProcId,
	   			ProcStatic:  pis.ProcStatic,
	   			ProcMetrics: m,
	   		}
	   	}
	   	gr := NewGrouper(false, newNamer("g1"))

	   	// First call should return zero CPU/IO.
	   	p1 := newProc(1, "g1", ProcMetrics{1, 1.5, 2, 3, 4, 5, 8, 400, 9, 10, 1})
	   	_, err := gr.Update(procInfoIter(p1))
	   	c.Assert(err, IsNil)
	   	got1 := gr.Groups()
	   	want1 := GroupCountMap{
	   		"g1": GroupCounts{Counts{0, 0, 0, 0, 0, 0}, 1, 4, 5, time.Time{}, 8, 0.02, 1},
	   	}
	   	c.Check(got1, DeepEquals, want1)

	   	// Second call should return the delta CPU/IO from first observance,
	   	// as well as latest memory/proccount.
	   	p1.ProcMetrics = ProcMetrics{2, 2.5, 3, 4, 5, 6, 12, 400, 10, 11, 1}
	   	_, err = gr.Update(procInfoIter(p1))
	   	c.Assert(err, IsNil)
	   	got2 := gr.Groups()
	   	want2 := GroupCountMap{
	   		"g1": GroupCounts{Counts{1, 1, 1, 1, 1, 1}, 1, 5, 6, time.Time{}, 12, 0.03, 1},
	   	}
	   	c.Check(got2, DeepEquals, want2)

	   	// Third call: process hasn't changed, nor should our group stats.
	   	_, err = gr.Update(procInfoIter(p1))
	   	c.Assert(err, IsNil)
	   	got3 := gr.Groups()
	   	want3 := GroupCountMap{
	   		"g1": GroupCounts{Counts{1, 1, 1, 1, 1, 1}, 1, 5, 6, time.Time{}, 12, 0.03, 1},
	   	}
	   	c.Check(got3, DeepEquals, want3, Commentf("diff %s", pretty.Compare(got3, want3)))
	   }

	   // Test that Groups() reports on new CPU/IO activity, even if some processes in the
	   // group have gone away.
	   func (s MySuite) TestGrouperNonDecreasing(c *C) {
	   	newProc := func(pid int, name string, m ProcMetrics) ProcIdInfo {
	   		pis := newProcIdStatic(pid, 0, 0, name, nil)
	   		return ProcIdInfo{
	   			ProcId:      pis.ProcId,
	   			ProcStatic:  pis.ProcStatic,
	   			ProcMetrics: m,
	   		}
	   	}
	   	gr := NewGrouper(false, newNamer("g1", "g2"))
	   	p1 := newProc(1, "g1", ProcMetrics{1, 1.5, 2, 3, 4, 5, 4, 400, 6, 7, 1})
	   	p2 := newProc(2, "g2", ProcMetrics{2, 2.5, 3, 4, 5, 6, 40, 400, 7, 8, 1})

	   	_, err := gr.Update(procInfoIter(p1, p2))
	   	c.Assert(err, IsNil)

	   	got1 := gr.Groups()
	   	want1 := GroupCountMap{
	   		"g1": GroupCounts{Counts{0, 0, 0, 0, 0, 0}, 1, 4, 5, time.Time{}, 4, 0.01, 1},
	   		"g2": GroupCounts{Counts{0, 0, 0, 0, 0, 0}, 1, 5, 6, time.Time{}, 40, 0.1, 1},
	   	}
	   	c.Check(got1, DeepEquals, want1, Commentf("diff %s", pretty.Compare(got1, want1)))

	   	// Now add a new proc p3 to g2, and increment p1/p2's metrics.
	   	p1.ProcMetrics = ProcMetrics{2, 2.5, 3, 4, 5, 6, 8, 400, 7, 8, 1}
	   	p2.ProcMetrics = ProcMetrics{4, 4.5, 5, 6, 7, 8, 80, 400, 9, 10, 1}
	   	p3 := newProc(3, "g2", ProcMetrics{1, 1, 1, 1, 1, 1, 8, 400, 1, 1, 2})
	   	_, err = gr.Update(procInfoIter(p1, p2, p3))
	   	c.Assert(err, IsNil)

	   	got2 := gr.Groups()
	   	want2 := GroupCountMap{
	   		"g1": GroupCounts{Counts{1, 1, 1, 1, 1, 1}, 1, 5, 6, time.Time{}, 8, 0.02, 1},
	   		"g2": GroupCounts{Counts{2, 2, 2, 2, 2, 2}, 2, 8, 9, time.Time{}, 88, 0.2, 3},
	   	}
	   	c.Check(got2, DeepEquals, want2, Commentf("diff %s", pretty.Compare(got2, want2)))

	   	// Now update p3's metrics and kill p2.
	   	p3.ProcMetrics = ProcMetrics{2, 2.5, 3, 4, 5, 6, 8, 400, 7, 8, 2}
	   	_, err = gr.Update(procInfoIter(p1, p3))
	   	c.Assert(err, IsNil)

	   	got3 := gr.Groups()
	   	want3 := GroupCountMap{
	   		"g1": GroupCounts{Counts{1, 1, 1, 1, 1, 1}, 1, 5, 6, time.Time{}, 8, 0.02, 1},
	   		"g2": GroupCounts{Counts{3, 3.5, 4, 5, 8, 9}, 1, 5, 6, time.Time{}, 8, 0.02, 2},
	   	}
	   	c.Check(got3, DeepEquals, want3, Commentf("diff %s", pretty.Compare(got3, want3)))

	   	// Now update p3's metrics and kill p1.
	   	p3.ProcMetrics = ProcMetrics{4, 4, 4, 4, 2, 1, 4, 400, 4, 4, 2}
	   	_, err = gr.Update(procInfoIter(p3))
	   	c.Assert(err, IsNil)

	   	got4 := gr.Groups()
	   	want4 := GroupCountMap{
	   		"g1": GroupCounts{Counts{1, 1, 1, 1, 1, 1}, 0, 0, 0, time.Time{}, 0, 0, 0},
	   		"g2": GroupCounts{Counts{5, 5, 5, 5, 5, 5}, 1, 2, 1, time.Time{}, 4, 0.01, 2},
	   	}
	   	c.Check(got4, DeepEquals, want4, Commentf("diff %s\n%s", pretty.Compare(got4, want4), pretty.Sprint(gr)))

}
*/
