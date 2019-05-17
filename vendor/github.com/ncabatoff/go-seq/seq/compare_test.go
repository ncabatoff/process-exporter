package seq

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

type simple struct {
	b bool
	i int
	u uint
	f float64
	s string
	a []byte
	p *bool
	l []int
	m map[string]int
}

func TestCompareSimple(t *testing.T) {
	var vtrue, vfalse bool
	vtrue = true
	var tests = []struct {
		msg    string
		s1, s2 simple
		want   int
	}{
		{msg: "zero", want: 0},
		{msg: "s1 bool", s1: simple{b: true}, want: 1},
		{msg: "s2 bool", s2: simple{b: true}, want: -1},
		{msg: "s1 int", s1: simple{i: 1}, want: 1},
		{msg: "s2 int", s2: simple{i: 1}, want: -1},
		{msg: "s1 uint", s1: simple{u: 1}, want: 1},
		{msg: "s2 uint", s2: simple{u: 1}, want: -1},
		{msg: "s1 float", s1: simple{f: 1}, want: 1},
		{msg: "s2 float", s2: simple{f: 1}, want: -1},
		{msg: "s1 string", s1: simple{s: "a"}, want: 1},
		{msg: "s2 string", s2: simple{s: "a"}, want: -1},
		{msg: "s1 []byte", s1: simple{a: []byte{'a'}}, want: 1},
		{msg: "s2 []byte", s2: simple{a: []byte{'a'}}, want: -1},
		{msg: "s1 ptr vs nil", s1: simple{p: &vfalse}, want: 1},
		{msg: "s1 ptr", s1: simple{p: &vtrue}, s2: simple{p: &vfalse}, want: 1},
		{msg: "s2 ptr", s1: simple{p: &vfalse}, s2: simple{p: &vtrue}, want: -1},
		{msg: "s1 slice", s1: simple{l: []int{1}}, want: 1},
		{msg: "s2 slice", s2: simple{l: []int{1}}, want: -1},
		{msg: "s1 slice", s1: simple{l: []int{0}}, s2: simple{l: []int{1}}, want: -1},
		{msg: "s1 map", s1: simple{m: map[string]int{"a": 1}}, want: 1},
		{msg: "s2 map", s2: simple{m: map[string]int{"a": 1}}, want: -1},
		{msg: "s1 map", s1: simple{m: map[string]int{"a": 1}}, s2: simple{m: map[string]int{"a": 2}}, want: -1},
		{msg: "s1 map", s1: simple{m: map[string]int{"a": 1}}, s2: simple{m: map[string]int{"b": 1}}, want: -1},
	}

	opts := cmp.AllowUnexported(simple{})
	for i, test := range tests {
		got := Compare(test.s1, test.s2)
		if got != test.want {
			diff := cmp.Diff(test.s1, test.s2, opts)
			t.Errorf("%d %s: got=%d, want=%d, diff:\n%s", i, test.msg, got, test.want, diff)
		}
	}
}

type nested struct {
	s simple
	a []simple
	p []*simple
}

func TestCompareNested(t *testing.T) {
	var tests = []struct {
		msg    string
		s1, s2 nested
		want   int
	}{
		{msg: "zero", want: 0},
		{msg: "s1 struct", s1: nested{s: simple{i: 1}}, want: 1},
		{msg: "s2 struct", s2: nested{s: simple{i: 1}}, want: -1},
		{msg: "s1 slice struct", s1: nested{a: []simple{simple{i: 1}}}, want: 1},
		{msg: "s2 slice struct", s2: nested{a: []simple{simple{i: 1}}}, want: -1},
		{msg: "s1 slice struct", s1: nested{a: []simple{simple{i: 1}}}, s2: nested{a: []simple{simple{i: 0}}}, want: 1},
		{msg: "s2 slice struct", s1: nested{a: []simple{simple{i: 0}}}, s2: nested{a: []simple{simple{i: 1}}}, want: -1},
		{msg: "s2 slice struct", s1: nested{p: []*simple{&simple{i: 0}}}, s2: nested{p: []*simple{&simple{i: 1}}}, want: -1},
	}

	opts := cmp.AllowUnexported(simple{}, nested{})
	for i, test := range tests {
		got := Compare(test.s1, test.s2)
		if got != test.want {
			diff := cmp.Diff(test.s1, test.s2, opts)
			t.Errorf("%d %s: got=%d, want=%d, diff:\n%s", i, test.msg, got, test.want, diff)
		}

	}
}

func TestCompareSlice(t *testing.T) {
	var tests = []struct {
		msg    string
		s1, s2 []int
		want   int
	}{
		{msg: "zero", want: 0},
		{msg: "equal", s1: []int{1}, s2: []int{1}, want: 0},
		{msg: "s1 smaller", s1: []int{-1}, s2: []int{1}, want: -1},
		{msg: "s1 larger", s1: []int{-1}, s2: []int{-2}, want: 1},
		{msg: "s1 shorter", s1: []int{1}, s2: []int{1, 1}, want: -1},
		{msg: "s1 longer", s1: []int{1, 1}, s2: []int{1}, want: 1},
	}

	opts := cmp.AllowUnexported(simple{}, nested{})
	for i, test := range tests {
		got := Compare(test.s1, test.s2)
		if got != test.want {
			diff := cmp.Diff(test.s1, test.s2, opts)
			t.Errorf("%d %s: got=%d, want=%d, diff:\n%s", i, test.msg, got, test.want, diff)
		}

	}
}
