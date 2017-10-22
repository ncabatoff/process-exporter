package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/prometheus/procfs"
)

var ErrProcNotExist = fmt.Errorf("process does not exist")

func newProcIdStatic(pid, ppid int, startTime uint64, name string, cmdline []string) ProcIdStatic {
	return ProcIdStatic{
		ProcId{pid, startTime},
		ProcStatic{name, cmdline, ppid, time.Unix(int64(startTime), 0).UTC()},
	}
}

type (
	// ProcId uniquely identifies a process.
	ProcId struct {
		// UNIX process id
		Pid int
		// The time the process started after system boot, the value is expressed
		// in clock ticks.
		StartTimeRel uint64
	}

	// ProcStatic contains data read from /proc/pid/*
	ProcStatic struct {
		Name      string
		Cmdline   []string
		ParentPid int
		StartTime time.Time
	}

	Counts struct {
		CpuUserTime     float64
		CpuSystemTime   float64
		ReadBytes       uint64
		WriteBytes      uint64
		MajorPageFaults uint64
		MinorPageFaults uint64
	}

	Memory struct {
		ResidentBytes uint64
		VirtualBytes  uint64
	}

	Filedesc struct {
		Open  int64 // -1 if we don't know
		Limit uint64
	}

	// ProcMetrics contains data read from /proc/pid/*
	ProcMetrics struct {
		Counts
		Memory
		Filedesc
		NumThreads uint64
	}

	ProcThread struct {
		ThreadName string
		Counts
	}

	ProcIdStatic struct {
		ProcId
		ProcStatic
	}

	ProcInfo struct {
		ProcStatic
		ProcMetrics
	}

	ProcIdInfo struct {
		ProcId
		ProcStatic
		ProcMetrics
	}

	// ProcIdInfoThreads struct {
	// 	ProcIdInfo
	// 	Threads []ProcThread
	// }

	// Proc wraps the details of the underlying procfs-reading library.
	// Any of these methods may fail if the process has disapeared.
	// We try to return as much as possible rather than an error, e.g.
	// if some /proc files are unreadable.
	Proc interface {
		// GetPid() returns the POSIX PID (process id).  They may be reused over time.
		GetPid() int
		// GetProcId() returns (pid,starttime), which can be considered a unique process id.
		GetProcId() (ProcId, error)
		// GetStatic() returns various details read from files under /proc/<pid>/.  Technically
		// name may not be static, but we'll pretend it is.
		GetStatic() (ProcStatic, error)
		// GetMetrics() returns various metrics read from files under /proc/<pid>/.
		// It returns an error on complete failure.  Otherwise, it returns metrics
		// and 0 on complete success, 1 if some (like I/O) couldn't be read.
		GetMetrics() (ProcMetrics, int, error)
		GetCounts() (Counts, int, error)
		// GetThreads() ([]ProcThread, error)
	}

	// proccache implements the Proc interface by acting as wrapper for procfs.Proc
	// that caches results of some reads.
	proccache struct {
		procfs.Proc
		procid  *ProcId
		stat    *procfs.ProcStat
		cmdline []string
		io      *procfs.ProcIO
		fs      *FS
	}

	proc struct {
		proccache
	}

	// procs is a fancier []Proc that saves on some copying.
	procs interface {
		get(int) Proc
		length() int
	}

	// procfsprocs implements procs using procfs.
	procfsprocs struct {
		Procs []procfs.Proc
		fs    *FS
	}

	// ProcIter is an iterator over a sequence of procs.
	ProcIter interface {
		// Next returns true if the iterator is not exhausted.
		Next() bool
		// Close releases any resources the iterator uses.
		Close() error
		// The iterator satisfies the Proc interface.
		Proc
	}

	// procIterator implements the ProcIter interface
	procIterator struct {
		// procs is the list of Proc we're iterating over.
		procs
		// idx is the current iteration, i.e. it's an index into procs.
		idx int
		// err is set with an error when Next() fails.  It is not affected by failures accessing
		// the current iteration variable, e.g. with GetProcId.
		err error
		// Proc is the current iteration variable, or nil if Next() has never been called or the
		// iterator is exhausted.
		Proc
	}
)

func (c *Counts) Add(c2 Counts) {
	c.CpuUserTime += c2.CpuUserTime
	c.CpuSystemTime += c2.CpuSystemTime
	c.ReadBytes += c2.ReadBytes
	c.WriteBytes += c2.WriteBytes
	c.MajorPageFaults += c2.MajorPageFaults
	c.MinorPageFaults += c2.MinorPageFaults
}

func (c *Counts) Sub(c2 Counts) {
	c.CpuUserTime -= c2.CpuUserTime
	c.CpuSystemTime -= c2.CpuSystemTime
	c.ReadBytes -= c2.ReadBytes
	c.WriteBytes -= c2.WriteBytes
	c.MajorPageFaults -= c2.MajorPageFaults
	c.MinorPageFaults -= c2.MinorPageFaults
}

//func (p ProcIdInfoThreads) GetThreads() ([]ProcThread, error) {
//	return p.Threads, nil
//}

// Info reads the ProcIdInfo for a proc and returns it or a zero value plus
// an error.
func Info(p Proc) (ProcIdInfo, error) {
	id, err := p.GetProcId()
	if err != nil {
		return ProcIdInfo{}, err
	}
	static, err := p.GetStatic()
	if err != nil {
		return ProcIdInfo{}, err
	}
	metrics, _, err := p.GetMetrics()
	if err != nil {
		return ProcIdInfo{}, err
	}
	return ProcIdInfo{id, static, metrics}, nil
}

func (p ProcIdInfo) GetPid() int {
	return p.ProcId.Pid
}

func (p ProcIdInfo) GetProcId() (ProcId, error) {
	return p.ProcId, nil
}

func (p ProcIdInfo) GetStatic() (ProcStatic, error) {
	return p.ProcStatic, nil
}

func (p ProcIdInfo) GetCounts() (Counts, int, error) {
	return p.ProcMetrics.Counts, 0, nil
}

func (p ProcIdInfo) GetMetrics() (ProcMetrics, int, error) {
	return p.ProcMetrics, 0, nil
}

func (p *proccache) GetPid() int {
	return p.Proc.PID
}

func (p *proccache) GetStat() (procfs.ProcStat, error) {
	if p.stat == nil {
		stat, err := p.Proc.NewStat()
		if err != nil {
			return procfs.ProcStat{}, err
		}
		p.stat = &stat
	}

	return *p.stat, nil
}

func (p *proccache) GetProcId() (ProcId, error) {
	if p.procid == nil {
		stat, err := p.GetStat()
		if err != nil {
			return ProcId{}, err
		}
		p.procid = &ProcId{Pid: p.GetPid(), StartTimeRel: stat.Starttime}
	}

	return *p.procid, nil
}

func (p *proccache) GetCmdLine() ([]string, error) {
	if p.cmdline == nil {
		cmdline, err := p.Proc.CmdLine()
		if err != nil {
			return nil, err
		}
		p.cmdline = cmdline
	}
	return p.cmdline, nil
}

func (p *proccache) GetIo() (procfs.ProcIO, error) {
	if p.io == nil {
		io, err := p.Proc.NewIO()
		if err != nil {
			return procfs.ProcIO{}, err
		}
		p.io = &io
	}
	return *p.io, nil
}

// GetStatic returns the ProcStatic corresponding to this proc.
func (p *proccache) GetStatic() (ProcStatic, error) {
	// /proc/<pid>/cmdline is normally world-readable.
	cmdline, err := p.GetCmdLine()
	if err != nil {
		return ProcStatic{}, err
	}
	// /proc/<pid>/stat is normally world-readable.
	stat, err := p.GetStat()
	if err != nil {
		return ProcStatic{}, err
	}
	startTime := time.Unix(p.fs.BootTime, 0).UTC()
	startTime = startTime.Add(time.Second / userHZ * time.Duration(stat.Starttime))
	return ProcStatic{
		Name:      stat.Comm,
		Cmdline:   cmdline,
		ParentPid: stat.PPID,
		StartTime: startTime,
	}, nil
}

func (p proc) GetCounts() (Counts, int, error) {
	stat, err := p.GetStat()
	if err != nil {
		if err == os.ErrNotExist {
			err = ErrProcNotExist
		}
		return Counts{}, 0, err
	}

	io, err := p.GetIo()
	softerrors := 0
	if err != nil {
		softerrors++
	}
	return Counts{
		CpuUserTime:     float64(stat.UTime) / userHZ,
		CpuSystemTime:   float64(stat.STime) / userHZ,
		ReadBytes:       io.ReadBytes,
		WriteBytes:      io.WriteBytes,
		MajorPageFaults: uint64(stat.MajFlt),
		MinorPageFaults: uint64(stat.MinFlt),
	}, softerrors, nil
}

// GetMetrics returns the current metrics for the proc.  The results are
// not cached.
func (p proc) GetMetrics() (ProcMetrics, int, error) {
	counts, softerrors, err := p.GetCounts()
	if err != nil {
		return ProcMetrics{}, 0, err
	}

	// We don't need to check for error here because p will have cached
	// the successful result of calling GetStat in GetCounts.
	// Since GetMetrics isn't a pointer receiver method, our callers
	// won't see the effect of the caching between calls.
	stat, _ := p.GetStat()

	numfds, err := p.Proc.FileDescriptorsLen()
	if err != nil {
		numfds = -1
		softerrors |= 1
	}

	limits, err := p.Proc.NewLimits()
	if err != nil {
		return ProcMetrics{}, 0, err
	}

	return ProcMetrics{
		Counts: counts,
		Memory: Memory{
			ResidentBytes: uint64(stat.ResidentMemory()),
			VirtualBytes:  uint64(stat.VirtualMemory()),
		},
		Filedesc: Filedesc{
			Open:  int64(numfds),
			Limit: uint64(limits.OpenFiles),
		},
		NumThreads: uint64(stat.NumThreads),
	}, softerrors, nil
}

func (p proc) GetThreads() ([]ProcThread, error) {
	fs, err := p.fs.ThreadFs(p.PID)
	if err != nil {
		return nil, err
	}

	threads := []ProcThread{}
	iter := fs.AllProcs()
	for iter.Next() {
		static, err := iter.GetStatic()
		if err != nil {
			continue
		}
		metrics, _, err := iter.GetCounts()
		if err != nil {
			continue
		}
		threads = append(threads, ProcThread{
			ThreadName: static.Name,
			Counts:     metrics,
		})
	}
	err = iter.Close()
	if err != nil {
		return nil, err
	}

	return threads, nil
}

type FS struct {
	procfs.FS
	BootTime   int64
	MountPoint string
}

// See https://github.com/prometheus/procfs/blob/master/proc_stat.go for details on userHZ.
const userHZ = 100

// NewFS returns a new FS mounted under the given mountPoint. It will error
// if the mount point can't be read.
func NewFS(mountPoint string) (*FS, error) {
	fs, err := procfs.NewFS(mountPoint)
	if err != nil {
		return nil, err
	}
	stat, err := fs.NewStat()
	if err != nil {
		return nil, err
	}
	return &FS{fs, stat.BootTime, mountPoint}, nil
}

func (fs *FS) ThreadFs(pid int) (*FS, error) {
	mountPoint := filepath.Join(fs.MountPoint, strconv.Itoa(pid), "task")
	tfs, err := procfs.NewFS(mountPoint)
	if err != nil {
		return nil, err
	}
	return &FS{tfs, fs.BootTime, mountPoint}, nil
}

func (fs *FS) AllProcs() ProcIter {
	procs, err := fs.FS.AllProcs()
	if err != nil {
		err = fmt.Errorf("Error reading procs: %v", err)
	}
	return &procIterator{procs: procfsprocs{procs, fs}, err: err, idx: -1}
}

func (p procfsprocs) get(i int) Proc {
	return &proc{proccache{Proc: p.Procs[i], fs: p.fs}}
}

func (p procfsprocs) length() int {
	return len(p.Procs)
}

func (pi *procIterator) Next() bool {
	pi.idx++
	if pi.idx < pi.procs.length() {
		pi.Proc = pi.procs.get(pi.idx)
	} else {
		pi.Proc = nil
	}
	return pi.idx < pi.procs.length()
}

func (pi *procIterator) Close() error {
	pi.Next()
	pi.procs = nil
	pi.Proc = nil
	return pi.err
}
