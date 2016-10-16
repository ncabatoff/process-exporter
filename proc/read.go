package proc

import (
	"errors"
	"fmt"
	"github.com/prometheus/procfs"
)

var (
	ErrUnnamed   = errors.New("unnamed proc")
	ErrNoCommand = errors.New("proc has empty cmdline")
)

type (
	// procSum contains data read from /proc/pid/*
	procSum struct {
		name        string
		cmdline     []string
		cpu         float64
		readbytes   uint64
		writebytes  uint64
		startTime   uint64
		memresident uint64
		memvirtual  uint64
		// memswap     uint64
	}
)

func (ps procSum) String() string {
	cmd := ps.cmdline
	if len(cmd) > 20 {
		cmd = cmd[:20]
	}
	return fmt.Sprintf("%20s %20s %7.0f %12d %12d", ps.name, cmd, ps.cpu, ps.readbytes, ps.writebytes)
}

func getProcSummary(proc procfs.Proc) (procSum, error) {
	var psum procSum

	stat, err := proc.NewStat()
	if err != nil {
		return psum, err
	}

	name := stat.Comm
	if name == "" {
		// these all appear to be kernel processes, which people generally don't care about
		// monitoring directly (i.e. the system OS metrics suffice)
		return psum, ErrUnnamed
	}

	cmdline, err := proc.CmdLine()
	if err != nil {
		return psum, err
	}

	if len(cmdline) == 0 {
		// these all appear to be kernel processes, which people generally don't care about
		// monitoring directly (i.e. the system OS metrics suffice)
		return psum, ErrNoCommand
	}

	ios, err := proc.NewIO()
	if err != nil {
		return psum, err
	}

	ctime := stat.Starttime

	psum.name = name
	psum.cmdline = cmdline
	psum.cpu = stat.CPUTime()
	psum.writebytes = ios.WriteBytes
	psum.readbytes = ios.ReadBytes
	psum.startTime = ctime
	psum.memresident = uint64(stat.ResidentMemory())
	psum.memvirtual = uint64(stat.VirtualMemory())
	// psum.memswap = meminfo.Swap

	return psum, nil
}
