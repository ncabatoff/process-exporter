package proc

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	. "gopkg.in/check.v1"
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

func (s MySuite) TestReadFixture(c *C) {
	procs := allprocs("../fixtures")
	count := 0
	for procs.Next() {
		count++
		c.Assert(count, Equals, 1)

		c.Check(procs.GetPid(), Equals, 14804)

		procid, err := procs.GetProcId()
		c.Assert(err, IsNil)
		c.Check(procid, Equals, ProcId{Pid: 14804, StartTimeRel: 0x4f27b})

		static, err := procs.GetStatic()
		c.Assert(err, IsNil)
		c.Check(static.Name, Equals, "process-exporte")
		c.Check(static.Cmdline, DeepEquals, []string{"./process-exporter", "-procnames", "bash"})
		c.Check(static.ParentPid, Equals, 10884)
		c.Check(static.StartTime.UTC().Format(time.RFC3339), Equals, "2017-10-19T22:52:51Z")

		metrics, softerrs, err := procs.GetMetrics()
		c.Assert(err, IsNil)
		c.Assert(softerrs, Equals, 0)
		c.Check(metrics.CpuUserTime, Equals, 0.1)
		c.Check(metrics.CpuSystemTime, Equals, 0.04)
		c.Check(metrics.ReadBytes, Equals, uint64(1814455))
		c.Check(metrics.WriteBytes, Equals, uint64(0))
		c.Check(metrics.ResidentBytes, Equals, uint64(0x7b1000))
		c.Check(metrics.VirtualBytes, Equals, uint64(0x1061000))
		c.Check(metrics.Filedesc, Equals, Filedesc{5, 0x400})
		c.Check(metrics.MajorPageFaults, Equals, uint64(0x2ff))
		c.Check(metrics.MinorPageFaults, Equals, uint64(0x643))
		c.Check(metrics.NumThreads, Equals, uint64(7))
	}
	err := procs.Close()
	c.Assert(err, IsNil)
	c.Check(count, Not(Equals), 0)
}

// Basic test of proc reading: does AllProcs return at least two procs, one of which is us.
func (s MySuite) TestAllProcs(c *C) {
	procs := allprocs("/proc")
	count := 0
	for procs.Next() {
		count++
		if procs.GetPid() != os.Getpid() {
			continue
		}
		procid, err := procs.GetProcId()
		c.Assert(err, IsNil)
		c.Check(procid.Pid, Equals, os.Getpid())
		static, err := procs.GetStatic()
		c.Assert(err, IsNil)
		c.Check(static.ParentPid, Equals, os.Getppid())
	}
	err := procs.Close()
	c.Assert(err, IsNil)
	c.Check(count, Not(Equals), 0)
}

// Verify that pid 1 doesn't provide I/O or FD stats.  This test
// fails if pid 1 is owned by the same user running the tests.
func (s MySuite) TestMissingIo(c *C) {
	procs := allprocs("/proc")
	for procs.Next() {
		if procs.GetPid() != 1 {
			continue
		}
		met, softerrs, err := procs.GetMetrics()
		c.Assert(err, IsNil)
		c.Assert(softerrs, Equals, 1)
		c.Check(met.ReadBytes, Equals, uint64(0))
		c.Check(met.WriteBytes, Equals, uint64(0))
		c.Check(met.ResidentBytes, Not(Equals), 0)
		c.Check(met.Filedesc.Limit, Not(Equals), 0)
		return
	}
}

// Test that we can observe the absence of a child process before it spawns and after it exits,
// and its presence during its lifetime.
func (s MySuite) TestAllProcsSpawn(c *C) {
	childprocs := func() ([]ProcIdStatic, error) {
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
			return nil, err
		}
		return found, nil
	}

	children1, err := childprocs()
	c.Assert(err, IsNil)

	cmd := exec.Command("/bin/cat")
	wc, err := cmd.StdinPipe()
	c.Assert(err, IsNil)
	err = cmd.Start()
	c.Assert(err, IsNil)

	children2, err := childprocs()
	c.Assert(err, IsNil)

	err = wc.Close()
	c.Assert(err, IsNil)
	err = cmd.Wait()
	c.Assert(err, IsNil)

	children3, err := childprocs()
	c.Assert(err, IsNil)

	foundcat := func(procs []ProcIdStatic) bool {
		for _, proc := range procs {
			if proc.Name == "cat" {
				return true
			}
		}
		return false
	}

	c.Check(foundcat(children1), Equals, false)
	c.Check(foundcat(children2), Equals, true)
	c.Check(foundcat(children3), Equals, false)
}

func (s MySuite) TestIterator(c *C) {
	// create a new proc with zero metrics, cmdline, starttime, ppid
	newProc := func(pid int, name string) ProcIdInfo {
		pis := newProcIdStatic(pid, 0, 0, name, nil)
		return ProcIdInfo{
			ProcId:      pis.ProcId,
			ProcStatic:  pis.ProcStatic,
			ProcMetrics: ProcMetrics{},
		}
	}
	p1 := newProc(1, "p1")
	want1 := []ProcIdInfo{p1}
	pi1 := procInfoIter(want1...)
	got, err := consumeIter(pi1)
	c.Assert(err, IsNil)
	c.Check(got, DeepEquals, want1)

	p2 := newProc(2, "p2")
	want2 := []ProcIdInfo{p1, p2}
	pi2 := procInfoIter(want2...)
	got2, err := consumeIter(pi2)
	c.Assert(err, IsNil)
	c.Check(got2, DeepEquals, want2)
}
