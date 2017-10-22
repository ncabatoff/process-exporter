package proc

import common "github.com/ncabatoff/process-exporter"

// read everything in the iterator
func consumeIter(pi ProcIter) ([]ProcIdInfo, error) {
	infos := []ProcIdInfo{}
	for pi.Next() {
		info, err := Info(pi)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

type namer map[string]struct{}

func newNamer(names ...string) namer {
	nr := make(namer, len(names))
	for _, name := range names {
		nr[name] = struct{}{}
	}
	return nr
}

func (n namer) MatchAndName(nacl common.NameAndCmdline) (bool, string) {
	if _, ok := n[nacl.Name]; ok {
		return true, nacl.Name
	}
	return false, ""
}

func newProc(pid int, name string, m ProcMetrics) ProcIdInfo {
	pis := newProcIdStatic(pid, 0, 0, name, nil)
	return ProcIdInfo{
		ProcId:      pis.ProcId,
		ProcStatic:  pis.ProcStatic,
		ProcMetrics: m,
	}
}
