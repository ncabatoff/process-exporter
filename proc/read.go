package proc

import (
	"fmt"
	"github.com/prometheus/procfs"
)

type (
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
	}

	// ProcMetrics contains data read from /proc/pid/*
	ProcMetrics struct {
		CpuTime       float64
		ReadBytes     uint64
		WriteBytes    uint64
		ResidentBytes uint64
		VirtualBytes  uint64
	}

	ProcIdStatic struct {
		ProcId
		ProcStatic
	}

	ProcInfo struct {
		ProcStatic
		ProcMetrics
	}

	// Proc wraps the details of the underlying procfs-reading library.
	Proc interface {
		// GetPid() returns the POSIX PID (process id).  They may be reused over time.
		GetPid() int
		// GetProcId() returns (pid,starttime), which can be considered a unique process id.
		// It may fail if the caller doesn't have permission to read /proc/<pid>/stat, or if
		// the process has disapeared.
		GetProcId() (ProcId, error)
		// GetStatic() returns various details read from files under /proc/<pid>/.  Technically
		// name may not be static, but we'll pretend it is.
		// It may fail if the caller doesn't have permission to read those files, or if
		// the process has disapeared.
		GetStatic() (ProcStatic, error)
		// GetMetrics() returns various metrics read from files under /proc/<pid>/.
		// It may fail if the caller doesn't have permission to read those files, or if
		// the process has disapeared.
		GetMetrics() (ProcMetrics, error)
	}

	// AllProcs is an iterator over a sequence of procs.
	Procs interface {
		// Next returns true if the iterator is not exhausted.
		Next() bool
		// Close releases any resources the iterator uses.
		Close() error
		// The iterator satisfies the Proc interface.
		Proc
	}

	procIterator struct {
		procs   []procfs.Proc
		idx     int
		err     error
		procid  *ProcId
		stat    *procfs.ProcStat
		cmdline []string
		io      *procfs.ProcIO
	}
)

func AllProcs() Procs {
	procs, err := procfs.AllProcs()
	if err != nil {
		err = fmt.Errorf("Error reading procs: %v", err)
	}
	return &procIterator{procs: procs, err: err, idx: -1}
}

func (pi *procIterator) Next() bool {
	pi.idx++
	pi.stat, pi.cmdline, pi.io = nil, nil, nil
	return pi.idx < len(pi.procs)
}

func (pi *procIterator) Close() error {
	pi.Next()
	pi.procs = nil
	return pi.err
}

// GetPid() implements Proc.
func (pi *procIterator) GetPid() int {
	return pi.procs[pi.idx].PID
}

// GetStat() wraps and caches procfs.Proc.NewStat().
func (pi *procIterator) GetStat() (procfs.ProcStat, error) {
	if pi.stat != nil {
		return *pi.stat, nil
	}
	proc := pi.procs[pi.idx]
	stat, err := proc.NewStat()
	if err != nil {
		return procfs.ProcStat{}, err
	}
	pi.stat = &stat
	return stat, nil
}

// GetCmdLine() wraps and caches procfs.Proc.CmdLine().
func (pi *procIterator) GetCmdLine() ([]string, error) {
	if pi.cmdline != nil {
		return pi.cmdline, nil
	}
	proc := pi.procs[pi.idx]
	cmdline, err := proc.CmdLine()
	if err != nil {
		return nil, err
	}
	pi.cmdline = cmdline
	return cmdline, nil
}

// GetIo() wraps and caches procfs.Proc.NewIO().
func (pi *procIterator) GetIo() (procfs.ProcIO, error) {
	if pi.io != nil {
		return *pi.io, nil
	}
	proc := pi.procs[pi.idx]
	io, err := proc.NewIO()
	if err != nil {
		return procfs.ProcIO{}, err
	}
	pi.io = &io
	return io, nil
}

// GetProcId() implements Proc.
func (pi *procIterator) GetProcId() (ProcId, error) {
	stat, err := pi.GetStat()
	if err != nil {
		return ProcId{}, err
	}

	return ProcId{Pid: pi.GetPid(), StartTimeRel: stat.Starttime}, nil
}

// GetStatic() implements Proc.
func (pi *procIterator) GetStatic() (ProcStatic, error) {
	cmdline, err := pi.GetCmdLine()
	if err != nil {
		return ProcStatic{}, err
	}
	stat, err := pi.GetStat()
	if err != nil {
		return ProcStatic{}, err
	}
	return ProcStatic{
		Name:      stat.Comm,
		Cmdline:   cmdline,
		ParentPid: stat.PPID,
	}, nil
}

// GetMetrics() implements Proc.
func (pi *procIterator) GetMetrics() (ProcMetrics, error) {
	io, err := pi.GetIo()
	if err != nil {
		return ProcMetrics{}, err
	}
	stat, err := pi.GetStat()
	if err != nil {
		return ProcMetrics{}, err
	}
	return ProcMetrics{
		CpuTime:       stat.CPUTime(),
		ReadBytes:     io.ReadBytes,
		WriteBytes:    io.WriteBytes,
		ResidentBytes: uint64(stat.ResidentMemory()),
		VirtualBytes:  uint64(stat.VirtualMemory()),
	}, nil
}
