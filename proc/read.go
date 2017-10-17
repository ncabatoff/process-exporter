package proc

import (
	"fmt"
	"os"
	"time"

	"github.com/prometheus/procfs"
)

var ErrProcNotExist = fmt.Errorf("process does not exist")

func newProcIdStatic(pid, ppid int, startTime uint64, name string, cmdline []string) ProcIdStatic {
	return ProcIdStatic{ProcId{pid, startTime}, ProcStatic{name, cmdline, ppid, time.Time{}}}
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

	// ProcMetrics contains data read from /proc/pid/*
	ProcMetrics struct {
		CpuUserTime     float64
		CpuSystemTime   float64
		ReadBytes       int64 // -1 if unavailable
		WriteBytes      int64 // -1 if unavailable
		ResidentBytes   uint64
		VirtualBytes    uint64
		OpenFDs         int64 // -1 if unavailable
		MaxFDs          uint64
		MajorPageFaults uint64
		MinorPageFaults uint64
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
		GetMetrics() (ProcMetrics, error)
	}

	// proc is a wrapper for procfs.Proc that caches results of some reads and implements Proc.
	proc struct {
		procfs.Proc
		procid   *ProcId
		stat     *procfs.ProcStat
		cmdline  []string
		io       *procfs.ProcIO
		bootTime int64
	}

	procs interface {
		get(int) Proc
		length() int
	}

	procfsprocs struct {
		Procs    []procfs.Proc
		bootTime int64
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

	// procIterator implements the ProcIter interface using procfs.
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

	procIdInfos []ProcIdInfo
)

func procInfoIter(ps ...ProcIdInfo) ProcIter {
	return &procIterator{procs: procIdInfos(ps), idx: -1}
}

func Info(p Proc) (ProcIdInfo, error) {
	id, err := p.GetProcId()
	if err != nil {
		return ProcIdInfo{}, err
	}
	static, err := p.GetStatic()
	if err != nil {
		return ProcIdInfo{}, err
	}
	metrics, err := p.GetMetrics()
	if err != nil {
		return ProcIdInfo{}, err
	}
	return ProcIdInfo{id, static, metrics}, nil
}

func (p procIdInfos) get(i int) Proc {
	return &p[i]
}

func (p procIdInfos) length() int {
	return len(p)
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

func (p ProcIdInfo) GetMetrics() (ProcMetrics, error) {
	return p.ProcMetrics, nil
}

func (p procfsprocs) get(i int) Proc {
	return &proc{Proc: p.Procs[i], bootTime: p.bootTime}
}

func (p procfsprocs) length() int {
	return len(p.Procs)
}

func (p *proc) GetPid() int {
	return p.Proc.PID
}

func (p *proc) GetStat() (procfs.ProcStat, error) {
	if p.stat == nil {
		stat, err := p.Proc.NewStat()
		if err != nil {
			return procfs.ProcStat{}, err
		}
		p.stat = &stat
	}

	return *p.stat, nil
}

func (p *proc) GetProcId() (ProcId, error) {
	if p.procid == nil {
		stat, err := p.GetStat()
		if err != nil {
			return ProcId{}, err
		}
		p.procid = &ProcId{Pid: p.GetPid(), StartTimeRel: stat.Starttime}
	}

	return *p.procid, nil
}

func (p *proc) GetCmdLine() ([]string, error) {
	if p.cmdline == nil {
		cmdline, err := p.Proc.CmdLine()
		if err != nil {
			return nil, err
		}
		p.cmdline = cmdline
	}
	return p.cmdline, nil
}

func (p *proc) GetIo() (procfs.ProcIO, error) {
	if p.io == nil {
		io, err := p.Proc.NewIO()
		if err != nil {
			return procfs.ProcIO{}, err
		}
		p.io = &io
	}
	return *p.io, nil
}

func (p proc) GetStatic() (ProcStatic, error) {
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
	startTime := time.Unix(p.bootTime, 0)
	startTime = startTime.Add(time.Second / userHZ * time.Duration(stat.Starttime))
	return ProcStatic{
		Name:      stat.Comm,
		Cmdline:   cmdline,
		ParentPid: stat.PPID,
		StartTime: startTime,
	}, nil
}

func (p proc) GetMetrics() (ProcMetrics, error) {
	io, err := p.GetIo()
	readbytes := int64(io.ReadBytes)
	writebytes := int64(io.WriteBytes)
	if err != nil {
		// Tolerate missing IO stats if kernel doesn't provide them,
		// either because it wasn't built with support or due to permissions.
		readbytes = -1
		writebytes = -1
	}
	stat, err := p.GetStat()
	if err != nil {
		if err == os.ErrNotExist {
			err = ErrProcNotExist
		}
		return ProcMetrics{}, err
	}
	numfds, err := p.Proc.FileDescriptorsLen()
	if err != nil {
		numfds = -1
	}
	limits, err := p.NewLimits()
	if err != nil {
		return ProcMetrics{}, err
	}
	return ProcMetrics{
		CpuUserTime:     float64(stat.UTime) / userHZ,
		CpuSystemTime:   float64(stat.STime) / userHZ,
		ReadBytes:       readbytes,
		WriteBytes:      writebytes,
		ResidentBytes:   uint64(stat.ResidentMemory()),
		VirtualBytes:    uint64(stat.VirtualMemory()),
		OpenFDs:         int64(numfds),
		MaxFDs:          uint64(limits.OpenFiles),
		MajorPageFaults: uint64(stat.MajFlt),
		MinorPageFaults: uint64(stat.MinFlt),
	}, nil
}

type FS struct {
	procfs.FS
	BootTime int64
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
	return &FS{fs, stat.BootTime}, nil
}

func (fs *FS) AllProcs() ProcIter {
	procs, err := fs.FS.AllProcs()
	if err != nil {
		err = fmt.Errorf("Error reading procs: %v", err)
	}
	return &procIterator{procs: procfsprocs{procs, fs.BootTime}, err: err, idx: -1}
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
