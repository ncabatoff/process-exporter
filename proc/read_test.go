package proc

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type (
	// procIdInfos implements procs using a slice of already
	// populated ProcIdInfo.  Used for testing.
	procIdInfos []ProcIdInfo
)

func (p procIdInfos) get(i int) Proc {
	return &p[i]
}

func (p procIdInfos) length() int {
	return len(p)
}

func procInfoIter(ps ...ProcIdInfo) *procIterator {
	return &procIterator{procs: procIdInfos(ps), idx: -1}
}

func allprocs(procpath string) ProcIter {
	fs, err := NewFS(procpath)
	if err != nil {
		cwd, _ := os.Getwd()
		panic("can't read " + procpath + ", cwd=" + cwd + ", err=" + fmt.Sprintf("%v", err))
	}
	return fs.AllProcs()
}

func TestReadFixture(t *testing.T) {
	procs := allprocs("../fixtures")
	var pii ProcIdInfo

	count := 0
	for procs.Next() {
		count++
		var err error
		pii, err = Info(procs)
		noerr(t, err)
	}
	err := procs.Close()
	noerr(t, err)
	if count != 1 {
		t.Fatalf("got %d procs, want 1", count)
	}

	wantprocid := ProcId{Pid: 14804, StartTimeRel: 0x4f27b}
	if diff := cmp.Diff(pii.ProcId, wantprocid); diff != "" {
		t.Errorf("procid differs: (-got +want)\n%s", diff)
	}

	stime, _ := time.Parse(time.RFC3339Nano, "2017-10-19T22:52:51.19Z")
	wantstatic := ProcStatic{
		Name:      "process-exporte",
		Cmdline:   []string{"./process-exporter", "-procnames", "bash"},
		ParentPid: 10884,
		StartTime: stime,
	}
	if diff := cmp.Diff(pii.ProcStatic, wantstatic); diff != "" {
		t.Errorf("static differs: (-got +want)\n%s", diff)
	}

	wantmetrics := ProcMetrics{
		Counts: Counts{
			CpuUserTime:     0.1,
			CpuSystemTime:   0.04,
			ReadBytes:       1814455,
			WriteBytes:      0,
			MajorPageFaults: 0x2ff,
			MinorPageFaults: 0x643,
		},
		Memory: Memory{
			ResidentBytes: 0x7b1000,
			VirtualBytes:  0x1061000,
		},
		Filedesc: Filedesc{
			Open:  5,
			Limit: 0x400,
		},
		NumThreads: 7,
	}
	if diff := cmp.Diff(pii.ProcMetrics, wantmetrics); diff != "" {
		t.Errorf("metrics differs: (-got +want)\n%s", diff)
	}
}

func noerr(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("error: %v", err)
	}
}

// Basic test of proc reading: does AllProcs return at least two procs, one of which is us.
func TestAllProcs(t *testing.T) {
	procs := allprocs("/proc")
	count := 0
	for procs.Next() {
		count++
		if procs.GetPid() != os.Getpid() {
			continue
		}
		procid, err := procs.GetProcId()
		noerr(t, err)
		if procid.Pid != os.Getpid() {
			t.Errorf("got %d, want %d", procid.Pid, os.Getpid())
		}
		static, err := procs.GetStatic()
		noerr(t, err)
		if static.ParentPid != os.Getppid() {
			t.Errorf("got %d, want %d", static.ParentPid, os.Getppid())
		}
	}
	err := procs.Close()
	noerr(t, err)
	if count == 0 {
		t.Errorf("got %d, want 0", count)
	}
}

// Verify that pid 1 doesn't provide I/O or FD stats.  This test
// fails if pid 1 is owned by the same user running the tests.
func TestMissingIo(t *testing.T) {
	procs := allprocs("/proc")
	for procs.Next() {
		if procs.GetPid() != 1 {
			continue
		}
		met, softerrs, err := procs.GetMetrics()
		noerr(t, err)

		if softerrs != 1 {
			t.Errorf("got %d, want %d", softerrs, 1)
		}
		if met.ReadBytes != uint64(0) {
			t.Errorf("got %d, want %d", met.ReadBytes, 0)
		}
		if met.WriteBytes != uint64(0) {
			t.Errorf("got %d, want %d", met.WriteBytes, 0)
		}
		if met.ResidentBytes == uint64(0) {
			t.Errorf("got %d, want non-zero", met.ResidentBytes)
		}
		if met.Filedesc.Limit == uint64(0) {
			t.Errorf("got %d, want non-zero", met.Filedesc.Limit)
		}
		return
	}
}

// Test that we can observe the absence of a child process before it spawns and after it exits,
// and its presence during its lifetime.
func TestAllProcsSpawn(t *testing.T) {
	childprocs := func() []ProcIdStatic {
		found := []ProcIdStatic{}
		procs := allprocs("/proc")
		mypid := os.Getpid()
		for procs.Next() {
			procid, err := procs.GetProcId()
			if err != nil {
				continue
			}
			static, err := procs.GetStatic()
			if err != nil {
				continue
			}
			if static.ParentPid == mypid {
				found = append(found, ProcIdStatic{procid, static})
			}
		}
		err := procs.Close()
		if err != nil {
			t.Fatalf("error closing procs iterator: %v", err)
		}
		return found
	}

	foundcat := func(procs []ProcIdStatic) bool {
		for _, proc := range procs {
			if proc.Name == "cat" {
				return true
			}
		}
		return false
	}

	if foundcat(childprocs()) {
		t.Errorf("found cat before spawning it")
	}

	cmd := exec.Command("/bin/cat")
	wc, err := cmd.StdinPipe()
	noerr(t, err)
	err = cmd.Start()
	noerr(t, err)

	if !foundcat(childprocs()) {
		t.Errorf("didn't find cat after spawning it")
	}

	err = wc.Close()
	noerr(t, err)
	err = cmd.Wait()
	noerr(t, err)

	if foundcat(childprocs()) {
		t.Errorf("found cat after exit")
	}
}

func TestIterator(t *testing.T) {
	p1 := newProc(1, "p1", ProcMetrics{})
	p2 := newProc(2, "p2", ProcMetrics{})
	want := []ProcIdInfo{p1, p2}
	pis := procInfoIter(want...)
	got, err := consumeIter(pis)
	noerr(t, err)
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("procs differs: (-got +want)\n%s", diff)
	}
}
