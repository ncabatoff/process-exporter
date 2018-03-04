// +build linux

package proc

import (
	"testing"
)

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
