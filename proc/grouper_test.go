package proc

import (
	. "gopkg.in/check.v1"
)

type idnamer struct{}

func (i idnamer) Name(nacl NameAndCmdline) string {
	return nacl.Name
}

// Test core grouper functionality, i.e things not related to namers or parents.
func (s MySuite) TestGrouperBasic(c *C) {
	newProc := func(pid int, name string, m ProcMetrics) ProcIdInfo {
		pis := newProcIdStatic(pid, 0, 0, name, nil)
		return ProcIdInfo{
			ProcId:      pis.ProcId,
			ProcStatic:  pis.ProcStatic,
			ProcMetrics: m,
		}
	}
	gr := NewGrouper([]string{"g1", "g2"}, false, idnamer{})
	p1 := newProc(1, "g1", ProcMetrics{1, 2, 3, 4, 5})
	p2 := newProc(2, "g2", ProcMetrics{2, 3, 4, 5, 6})
	p3 := newProc(3, "g3", ProcMetrics{})

	err := gr.Update(procInfoIter(p1, p2, p3))
	c.Assert(err, IsNil)

	got1 := gr.Groups()
	want1 := map[string]Groupcounts{
		"g1": Groupcounts{Counts{0, 0, 0}, 1, 4, 5},
		"g2": Groupcounts{Counts{0, 0, 0}, 1, 5, 6},
	}
	c.Check(got1, DeepEquals, want1)

	// Now increment counts and memory and make sure group counts updated.
	p1.ProcMetrics = ProcMetrics{2, 3, 4, 5, 6}
	p2.ProcMetrics = ProcMetrics{4, 5, 6, 7, 8}

	err = gr.Update(procInfoIter(p1, p2, p3))
	c.Assert(err, IsNil)

	got2 := gr.Groups()
	want2 := map[string]Groupcounts{
		"g1": Groupcounts{Counts{1, 1, 1}, 1, 5, 6},
		"g2": Groupcounts{Counts{2, 2, 2}, 1, 7, 8},
	}
	c.Check(got2, DeepEquals, want2)

	// Now add a new proc and update p2's metrics.  The
	// counts for p4 won't be factored into the total yet
	// because we only add to counts starting with the
	// second time we see a proc.  Memory is affected though.
	p4 := newProc(4, "g2", ProcMetrics{1, 1, 1, 1, 1})
	p2.ProcMetrics = ProcMetrics{5, 6, 7, 8, 9}

	err = gr.Update(procInfoIter(p1, p2, p3, p4))
	c.Assert(err, IsNil)

	got3 := gr.Groups()
	want3 := map[string]Groupcounts{
		"g1": Groupcounts{Counts{1, 1, 1}, 1, 5, 6},
		"g2": Groupcounts{Counts{3, 3, 3}, 2, 9, 10},
	}
	c.Check(got3, DeepEquals, want3)

	p4.ProcMetrics = ProcMetrics{2, 2, 2, 2, 2}
	p2.ProcMetrics = ProcMetrics{6, 7, 8, 8, 9}

	err = gr.Update(procInfoIter(p1, p2, p3, p4))
	c.Assert(err, IsNil)

	got4 := gr.Groups()
	want4 := map[string]Groupcounts{
		"g1": Groupcounts{Counts{1, 1, 1}, 1, 5, 6},
		"g2": Groupcounts{Counts{5, 5, 5}, 2, 10, 11},
	}
	c.Check(got4, DeepEquals, want4)

}

// Test that if a proc is tracked, we track its descendants,
// and if not as before it gets ignored.  We won't bother
// testing metric accumulation since that should be covered
// by TestGrouperBasic.
func (s MySuite) TestGrouperParents(c *C) {
	newProc := func(pid, ppid int, name string) ProcIdInfo {
		pis := newProcIdStatic(pid, ppid, 0, name, nil)
		return ProcIdInfo{
			ProcId:      pis.ProcId,
			ProcStatic:  pis.ProcStatic,
			ProcMetrics: ProcMetrics{},
		}
	}
	gr := NewGrouper([]string{"g1", "g2"}, true, idnamer{})
	p1 := newProc(1, 0, "g1")
	p2 := newProc(2, 0, "g2")
	p3 := newProc(3, 0, "g3")

	err := gr.Update(procInfoIter(p1, p2, p3))
	c.Assert(err, IsNil)

	got1 := gr.Groups()
	want1 := map[string]Groupcounts{
		"g1": Groupcounts{Counts{}, 1, 0, 0},
		"g2": Groupcounts{Counts{}, 1, 0, 0},
	}
	c.Check(got1, DeepEquals, want1)

	// Now we'll give each of the procs a child and test that the count of procs
	// in each group is incremented.

	p4 := newProc(4, p1.Pid, "")
	p5 := newProc(5, p2.Pid, "")
	p6 := newProc(6, p3.Pid, "")

	err = gr.Update(procInfoIter(p1, p2, p3, p4, p5, p6))
	c.Assert(err, IsNil)

	got2 := gr.Groups()
	want2 := map[string]Groupcounts{
		"g1": Groupcounts{Counts{}, 2, 0, 0},
		"g2": Groupcounts{Counts{}, 2, 0, 0},
	}
	c.Check(got2, DeepEquals, want2)

	// Now we'll let p4 die, and give p5 a child and grandchild and great-grandchild.

	p7 := newProc(7, p5.Pid, "")
	p8 := newProc(8, p7.Pid, "")
	p9 := newProc(9, p8.Pid, "")

	err = gr.Update(procInfoIter(p1, p2, p3, p5, p6, p7, p8, p9))
	c.Assert(err, IsNil)

	got3 := gr.Groups()
	want3 := map[string]Groupcounts{
		"g1": Groupcounts{Counts{}, 1, 0, 0},
		"g2": Groupcounts{Counts{}, 5, 0, 0},
	}
	c.Check(got3, DeepEquals, want3)
}
