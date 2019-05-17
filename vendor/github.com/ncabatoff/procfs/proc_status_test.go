// Copyright 2018 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package procfs

import (
	"testing"
)

func TestProcStatus(t *testing.T) {
	p, err := FS("fixtures").NewProc(26231)
	if err != nil {
		t.Fatal(err)
	}

	s, err := p.NewStatus()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		want int
		have int
	}{
		{name: "tid", want: 26231, have: s.TID},
		{name: "uidreal", want: 1000, have: s.UIDReal},
		{name: "vmlib", want: 69672, have: s.VmLibKB},
		{name: "nonvolctx", want: 361, have: s.NonvoluntaryCtxtSwitches},
	} {
		if test.want != test.have {
			t.Errorf("want %s %d, have %d", test.name, test.want, test.have)
		}
	}
}

func BenchmarkProcStatus(b *testing.B) {
	p, err := FS("fixtures").NewProc(26231)
	if err != nil {
		b.Fatal(err)
	}

	for n := 0; n < b.N; n++ {
		_, err := p.NewStatus()
		if err != nil {
			b.Fatal(err)
		}
	}
}
