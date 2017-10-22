package proc

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// Order of updates returned by tracker is not specified.
func tomap(ps []ProcUpdate) map[ProcUpdate]int {
	m := make(map[ProcUpdate]int)
	for _, p := range ps {
		if _, ok := m[p]; ok {
			m[p]++
		} else {
			m[p] = 1
		}
	}
	return m
}

// Verify that the tracker finds and tracks or ignores procs based on the
// namer, and that it can distinguish between two procs with the same pid
// but different start time.
func TestTrackerBasic(t *testing.T) {
	newProc := func(pid int, name string, startTime uint64) ProcIdInfo {
		pis := newProcIdStatic(pid, 0, startTime, name, nil)
		return ProcIdInfo{ProcId: pis.ProcId, ProcStatic: pis.ProcStatic}
	}

	p1, p2, p3 := 1, 2, 3
	n1, n2, n3, n4 := "g1", "g2", "g3", "g4"
	t1, t2, t3 := time.Unix(1, 0).UTC(), time.Unix(2, 0).UTC(), time.Unix(3, 0).UTC()

	tests := []struct {
		procs []ProcIdInfo
		want  []ProcUpdate
	}{
		{
			[]ProcIdInfo{newProc(p1, n1, 1), newProc(p3, n3, 1)},
			[]ProcUpdate{{GroupName: n1, Start: t1}},
		},
		{
			// p3 (ignored) has exited and p2 has appeared
			[]ProcIdInfo{newProc(p1, n1, 1), newProc(p2, n2, 2)},
			[]ProcUpdate{{GroupName: n1, Start: t1}, {GroupName: n2, Start: t2}},
		},
		{
			// p1 has exited and a new proc with a new name has taken its pid
			[]ProcIdInfo{newProc(p1, n4, 3), newProc(p2, n2, 2)},
			[]ProcUpdate{{GroupName: n4, Start: t3}, {GroupName: n2, Start: t2}},
		},
	}
	// Note that n3 should not be tracked according to our namer.
	tr := NewTracker(newNamer(n1, n2, n4), false)

	for i, tc := range tests {
		_, got, err := tr.Update(procInfoIter(tc.procs...))
		noerr(t, err)
		if diff := cmp.Diff(tomap(got), tomap(tc.want)); diff != "" {
			t.Errorf("%d: update differs: (-got +want)\n%s", i, diff)
		}
	}
}

// TestTrackerChildren verifies that when the tracker is asked to track
// children, processes not selected by the namer are still tracked if
// they're children of ones that are.
func TestTrackerChildren(t *testing.T) {
	newProc := func(pid int, name string, ppid int) ProcIdInfo {
		pis := newProcIdStatic(pid, ppid, 0, name, nil)
		return ProcIdInfo{ProcId: pis.ProcId, ProcStatic: pis.ProcStatic}
	}

	p1, p2, p3 := 1, 2, 3
	n1, n2, n3 := "g1", "g2", "g3"
	// In this test everything starts at time t1 for simplicity
	t1 := time.Unix(0, 0).UTC()

	tests := []struct {
		procs []ProcIdInfo
		want  []ProcUpdate
	}{
		{
			[]ProcIdInfo{newProc(p1, n1, 0), newProc(p2, n2, p1)},
			[]ProcUpdate{{GroupName: n2, Start: t1}},
		},
		{
			[]ProcIdInfo{newProc(p1, n1, 0), newProc(p2, n2, p1), newProc(p3, n3, p2)},
			[]ProcUpdate{{GroupName: n2, Start: t1}, {GroupName: n2, Start: t1}},
		},
	}
	// Only n2 and children of n2s should be tracked
	tr := NewTracker(newNamer(n2), true)

	for i, tc := range tests {
		_, got, err := tr.Update(procInfoIter(tc.procs...))
		noerr(t, err)
		if diff := cmp.Diff(tomap(got), tomap(tc.want)); diff != "" {
			t.Errorf("%d: update differs: (-got +want)\n%s", i, diff)
		}
	}
}

// TestTrackerMetrics verifies that the updates returned by the tracker
// match the input we're giving it.
func TestTrackerMetrics(t *testing.T) {
	p, n, tm := 1, "g1", time.Unix(0, 0).UTC()

	tests := []struct {
		proc ProcIdInfo
		want ProcUpdate
	}{
		{
			piinfo(p, n, Counts{1, 2, 3, 4, 5, 6}, Memory{7, 8}, Filedesc{1, 10}, 9),
			ProcUpdate{n, Counts{}, Memory{7, 8}, Filedesc{1, 10}, tm, 9},
		},
		{
			piinfo(p, n, Counts{2, 3, 4, 5, 6, 7}, Memory{1, 2}, Filedesc{2, 20}, 1),
			ProcUpdate{n, Counts{1, 1, 1, 1, 1, 1}, Memory{1, 2}, Filedesc{2, 20}, tm, 1},
		},
	}
	tr := NewTracker(newNamer(n), false)

	for i, tc := range tests {
		_, got, err := tr.Update(procInfoIter(tc.proc))
		noerr(t, err)
		if diff := cmp.Diff(got, []ProcUpdate{tc.want}); diff != "" {
			t.Errorf("%d: update differs: (-got +want)\n%s", i, diff)
		}
	}
}
