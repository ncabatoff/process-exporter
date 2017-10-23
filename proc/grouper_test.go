package proc

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type grouptest struct {
	grouper *Grouper
	procs   Iter
	want    GroupByName
}

//func (gt grouptest) run(c *C) {
//	_, err := gt.grouper.Update(gt.procs)
//	c.Assert(err, IsNil)
//
//	got := gt.grouper.curgroups()
//	c.Check(got, DeepEquals, gt.want, Commentf("diff %s", pretty.Compare(got, gt.want)))
//}

func run(t *testing.T, gr *Grouper, procs Iter) GroupByName {
	_, groups, err := gr.Update(procs)
	if err != nil {
		t.Fatalf("group.Update error: %v", err)
	}

	return groups
}

func piinfot(pid int, name string, c Counts, m Memory, f Filedesc, threads []Thread) IDInfo {
	pii := piinfo(pid, name, c, m, f, len(threads))
	pii.Threads = threads
	return pii
}

func piinfo(pid int, name string, c Counts, m Memory, f Filedesc, t int) IDInfo {
	id, static := newProcIDStatic(pid, 0, 0, name, nil)
	return IDInfo{
		ID:      id,
		Static:  static,
		Metrics: Metrics{c, m, f, uint64(t)},
	}
}

// TestGrouperBasic tests core Update/curgroups functionality on single-proc
// groups: the grouper adds to counts and updates the other tracked metrics like
// Memory.
func TestGrouperBasic(t *testing.T) {
	p1, p2 := 1, 2
	n1, n2 := "g1", "g2"
	starttime := time.Unix(0, 0).UTC()

	tests := []struct {
		procs []IDInfo
		want  GroupByName
	}{
		{
			[]IDInfo{
				piinfo(p1, n1, Counts{1, 2, 3, 4, 5, 6}, Memory{7, 8}, Filedesc{4, 400}, 2),
				piinfo(p2, n2, Counts{2, 3, 4, 5, 6, 7}, Memory{8, 9}, Filedesc{40, 400}, 3),
			},
			GroupByName{
				"g1": Group{Counts{}, 1, Memory{7, 8}, starttime, 4, 0.01, 2, nil},
				"g2": Group{Counts{}, 1, Memory{8, 9}, starttime, 40, 0.1, 3, nil},
			},
		},
		{
			[]IDInfo{
				piinfo(p1, n1, Counts{2, 3, 4, 5, 6, 7}, Memory{6, 7}, Filedesc{100, 400}, 4),
				piinfo(p2, n2, Counts{4, 5, 6, 7, 8, 9}, Memory{9, 8}, Filedesc{400, 400}, 2),
			},
			GroupByName{
				"g1": Group{Counts{1, 1, 1, 1, 1, 1}, 1, Memory{6, 7}, starttime, 100, 0.25, 4, nil},
				"g2": Group{Counts{2, 2, 2, 2, 2, 2}, 1, Memory{9, 8}, starttime, 400, 1, 2, nil},
			},
		},
	}

	gr := NewGrouper(newNamer(n1, n2), false, false)
	for i, tc := range tests {
		got := run(t, gr, procInfoIter(tc.procs...))
		if diff := cmp.Diff(got, tc.want); diff != "" {
			t.Errorf("%d: curgroups differs: (-got +want)\n%s", i, diff)
		}
	}
}

// TestGrouperProcJoin tests the appearance of a new process in a group,
// and that all procs metrics contribute to a group.
func TestGrouperProcJoin(t *testing.T) {
	p1, p2 := 1, 2
	n1, n2 := "g1", "g1"
	starttime := time.Unix(0, 0).UTC()

	tests := []struct {
		procs []IDInfo
		want  GroupByName
	}{
		{
			[]IDInfo{
				piinfo(p1, n1, Counts{1, 2, 3, 4, 5, 6}, Memory{3, 4}, Filedesc{4, 400}, 2),
			},
			GroupByName{
				"g1": Group{Counts{}, 1, Memory{3, 4}, starttime, 4, 0.01, 2, nil},
			},
		}, {
			// The counts for pid2 won't be factored into the total yet because we only add
			// to counts starting with the second time we see a proc. Memory and FDs are
			// affected though.
			[]IDInfo{
				piinfo(p1, n1, Counts{3, 4, 5, 6, 7, 8}, Memory{3, 4}, Filedesc{4, 400}, 2),
				piinfo(p2, n2, Counts{1, 1, 1, 1, 1, 1}, Memory{1, 2}, Filedesc{40, 400}, 3),
			},
			GroupByName{
				"g1": Group{Counts{2, 2, 2, 2, 2, 2}, 2, Memory{4, 6}, starttime, 44, 0.1, 5, nil},
			},
		}, {
			[]IDInfo{
				piinfo(p1, n1, Counts{4, 5, 6, 7, 8, 9}, Memory{1, 5}, Filedesc{4, 400}, 2),
				piinfo(p2, n2, Counts{2, 2, 2, 2, 2, 2}, Memory{2, 4}, Filedesc{40, 400}, 3),
			},
			GroupByName{
				"g1": Group{Counts{4, 4, 4, 4, 4, 4}, 2, Memory{3, 9}, starttime, 44, 0.1, 5, nil},
			},
		},
	}

	gr := NewGrouper(newNamer(n1), false, false)
	for i, tc := range tests {
		got := run(t, gr, procInfoIter(tc.procs...))
		if diff := cmp.Diff(got, tc.want); diff != "" {
			t.Errorf("%d: curgroups differs: (-got +want)\n%s", i, diff)
		}
	}
}

// TestGrouperNonDecreasing tests the disappearance of a process.  Its previous
// contribution to the counts should not go away when that happens.
func TestGrouperNonDecreasing(t *testing.T) {
	p1, p2 := 1, 2
	n1, n2 := "g1", "g1"
	starttime := time.Unix(0, 0).UTC()

	tests := []struct {
		procs []IDInfo
		want  GroupByName
	}{
		{
			[]IDInfo{
				piinfo(p1, n1, Counts{3, 4, 5, 6, 7, 8}, Memory{3, 4}, Filedesc{4, 400}, 2),
				piinfo(p2, n2, Counts{1, 1, 1, 1, 1, 1}, Memory{1, 2}, Filedesc{40, 400}, 3),
			},
			GroupByName{
				"g1": Group{Counts{}, 2, Memory{4, 6}, starttime, 44, 0.1, 5, nil},
			},
		}, {
			[]IDInfo{
				piinfo(p1, n1, Counts{4, 5, 6, 7, 8, 9}, Memory{1, 5}, Filedesc{4, 400}, 2),
			},
			GroupByName{
				"g1": Group{Counts{1, 1, 1, 1, 1, 1}, 1, Memory{1, 5}, starttime, 4, 0.01, 2, nil},
			},
		}, {
			[]IDInfo{},
			GroupByName{
				"g1": Group{Counts{1, 1, 1, 1, 1, 1}, 0, Memory{}, time.Time{}, 0, 0, 0, nil},
			},
		},
	}

	gr := NewGrouper(newNamer(n1), false, false)
	for i, tc := range tests {
		got := run(t, gr, procInfoIter(tc.procs...))
		if diff := cmp.Diff(got, tc.want); diff != "" {
			t.Errorf("%d: curgroups differs: (-got +want)\n%s", i, diff)
		}
	}
}

func TestGrouperThreads(t *testing.T) {
	p, n, tm := 1, "g1", time.Unix(0, 0).UTC()

	tests := []struct {
		proc IDInfo
		want GroupByName
	}{
		{
			piinfot(p, n, Counts{}, Memory{}, Filedesc{1, 1}, []Thread{
				{ThreadID(ID{p, 0}), "t1", Counts{1, 2, 3, 4, 5, 6}},
				{ThreadID(ID{p + 1, 0}), "t2", Counts{1, 1, 1, 1, 1, 1}},
			}),
			GroupByName{
				"g1": Group{Counts{}, 1, Memory{}, tm, 1, 1, 2, []Threads{
					Threads{"t1", 1, Counts{}},
					Threads{"t2", 1, Counts{}},
				}},
			},
		}, {
			piinfot(p, n, Counts{}, Memory{}, Filedesc{1, 1}, []Thread{
				{ThreadID(ID{p, 0}), "t1", Counts{2, 3, 4, 5, 6, 7}},
				{ThreadID(ID{p + 1, 0}), "t2", Counts{2, 2, 2, 2, 2, 2}},
				{ThreadID(ID{p + 2, 0}), "t2", Counts{1, 1, 1, 1, 1, 1}},
			}),
			GroupByName{
				"g1": Group{Counts{}, 1, Memory{}, tm, 1, 1, 3, []Threads{
					Threads{"t1", 1, Counts{1, 1, 1, 1, 1, 1}},
					Threads{"t2", 2, Counts{1, 1, 1, 1, 1, 1}},
				}},
			},
		}, {
			piinfot(p, n, Counts{}, Memory{}, Filedesc{1, 1}, []Thread{
				{ThreadID(ID{p + 1, 0}), "t2", Counts{4, 4, 4, 4, 4, 4}},
				{ThreadID(ID{p + 2, 0}), "t2", Counts{2, 3, 4, 5, 6, 7}},
			}),
			GroupByName{
				"g1": Group{Counts{}, 1, Memory{}, tm, 1, 1, 2, []Threads{
					Threads{"t2", 2, Counts{4, 5, 6, 7, 8, 9}},
				}},
			},
		},
	}

	opts := cmpopts.SortSlices(lessThreads)
	gr := NewGrouper(newNamer(n), false, true)
	for i, tc := range tests {
		got := run(t, gr, procInfoIter(tc.proc))
		if diff := cmp.Diff(got, tc.want, opts); diff != "" {
			t.Errorf("%d: curgroups differs: (-got +want)\n%s", i, diff)
		}
	}
}
