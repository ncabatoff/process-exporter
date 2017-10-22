package proc

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type grouptest struct {
	grouper *Grouper
	procs   ProcIter
	want    GroupByName
}

//func (gt grouptest) run(c *C) {
//	_, err := gt.grouper.Update(gt.procs)
//	c.Assert(err, IsNil)
//
//	got := gt.grouper.curgroups()
//	c.Check(got, DeepEquals, gt.want, Commentf("diff %s", pretty.Compare(got, gt.want)))
//}

func run(t *testing.T, gr *Grouper, procs ProcIter) GroupByName {
	_, groups, err := gr.Update(procs)
	if err != nil {
		t.Fatalf("group.Update error: %v", err)
	}

	return groups
}

func piinfo(pid int, name string, c Counts, m Memory, f Filedesc, t int) ProcIdInfo {
	pis := newProcIdStatic(pid, 0, 0, name, nil)
	return ProcIdInfo{
		ProcId:      pis.ProcId,
		ProcStatic:  pis.ProcStatic,
		ProcMetrics: ProcMetrics{c, m, f, uint64(t)},
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
		procs []ProcIdInfo
		want  GroupByName
	}{
		{
			[]ProcIdInfo{
				piinfo(p1, n1, Counts{1, 2, 3, 4, 5, 6}, Memory{7, 8}, Filedesc{4, 400}, 2),
				piinfo(p2, n2, Counts{2, 3, 4, 5, 6, 7}, Memory{8, 9}, Filedesc{40, 400}, 3),
			},
			GroupByName{
				"g1": Group{Counts{}, 1, Memory{7, 8}, starttime, 4, 0.01, 2},
				"g2": Group{Counts{}, 1, Memory{8, 9}, starttime, 40, 0.1, 3},
			},
		},
		{
			[]ProcIdInfo{
				piinfo(p1, n1, Counts{2, 3, 4, 5, 6, 7}, Memory{6, 7}, Filedesc{100, 400}, 4),
				piinfo(p2, n2, Counts{4, 5, 6, 7, 8, 9}, Memory{9, 8}, Filedesc{400, 400}, 2),
			},
			GroupByName{
				"g1": Group{Counts{1, 1, 1, 1, 1, 1}, 1, Memory{6, 7}, starttime, 100, 0.25, 4},
				"g2": Group{Counts{2, 2, 2, 2, 2, 2}, 1, Memory{9, 8}, starttime, 400, 1, 2},
			},
		},
	}

	gr := NewGrouper(false, newNamer(n1, n2))
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
		procs []ProcIdInfo
		want  GroupByName
	}{
		{
			[]ProcIdInfo{
				piinfo(p1, n1, Counts{1, 2, 3, 4, 5, 6}, Memory{3, 4}, Filedesc{4, 400}, 2),
			},
			GroupByName{
				"g1": Group{Counts{}, 1, Memory{3, 4}, starttime, 4, 0.01, 2},
			},
		}, {
			// The counts for pid2 won't be factored into the total yet because we only add
			// to counts starting with the second time we see a proc. Memory and FDs are
			// affected though.
			[]ProcIdInfo{
				piinfo(p1, n1, Counts{3, 4, 5, 6, 7, 8}, Memory{3, 4}, Filedesc{4, 400}, 2),
				piinfo(p2, n2, Counts{1, 1, 1, 1, 1, 1}, Memory{1, 2}, Filedesc{40, 400}, 3),
			},
			GroupByName{
				"g1": Group{Counts{2, 2, 2, 2, 2, 2}, 2, Memory{4, 6}, starttime, 44, 0.1, 5},
			},
		}, {
			[]ProcIdInfo{
				piinfo(p1, n1, Counts{4, 5, 6, 7, 8, 9}, Memory{1, 5}, Filedesc{4, 400}, 2),
				piinfo(p2, n2, Counts{2, 2, 2, 2, 2, 2}, Memory{2, 4}, Filedesc{40, 400}, 3),
			},
			GroupByName{
				"g1": Group{Counts{4, 4, 4, 4, 4, 4}, 2, Memory{3, 9}, starttime, 44, 0.1, 5},
			},
		},
	}

	gr := NewGrouper(false, newNamer(n1))
	for i, tc := range tests {
		got := run(t, gr, procInfoIter(tc.procs...))
		if diff := cmp.Diff(got, tc.want); diff != "" {
			t.Errorf("%d: curgroups differs: (-got +want)\n%s", i, diff)
		}
	}
}

// TestGrouperNonDecreasing tests the disappearance of a process.
func TestGrouperNonDecreasing(t *testing.T) {
	p1, p2 := 1, 2
	n1, n2 := "g1", "g1"
	starttime := time.Unix(0, 0).UTC()

	tests := []struct {
		procs []ProcIdInfo
		want  GroupByName
	}{
		{
			[]ProcIdInfo{
				piinfo(p1, n1, Counts{3, 4, 5, 6, 7, 8}, Memory{3, 4}, Filedesc{4, 400}, 2),
				piinfo(p2, n2, Counts{1, 1, 1, 1, 1, 1}, Memory{1, 2}, Filedesc{40, 400}, 3),
			},
			GroupByName{
				"g1": Group{Counts{}, 2, Memory{4, 6}, starttime, 44, 0.1, 5},
			},
		}, {
			[]ProcIdInfo{
				piinfo(p1, n1, Counts{4, 5, 6, 7, 8, 9}, Memory{1, 5}, Filedesc{4, 400}, 2),
			},
			GroupByName{
				"g1": Group{Counts{1, 1, 1, 1, 1, 1}, 1, Memory{1, 5}, starttime, 4, 0.01, 2},
			},
		}, {
			[]ProcIdInfo{},
			GroupByName{
				"g1": Group{Counts{1, 1, 1, 1, 1, 1}, 0, Memory{}, time.Time{}, 0, 0, 0},
			},
		},
	}

	gr := NewGrouper(false, newNamer(n1))
	for i, tc := range tests {
		got := run(t, gr, procInfoIter(tc.procs...))
		if diff := cmp.Diff(got, tc.want); diff != "" {
			t.Errorf("%d: curgroups differs: (-got +want)\n%s", i, diff)
		}
	}
}
