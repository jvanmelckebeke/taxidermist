package pyyaml

import (
	"os"
	"strings"
	"testing"
)

// testdata/scalars.golden is what PyYAML makes of each line in scalars.txt, written by
// scripts/pyyaml-parity/canon.py. Regenerate it there when adding a case.
func TestScalarsMatchPyYAML(t *testing.T) {
	cases, err := os.ReadFile("testdata/scalars.txt")
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/scalars.golden")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSuffix(string(golden), "\n"), "\n")
	lines := strings.Split(strings.TrimSuffix(string(cases), "\n"), "\n")
	if len(want) != len(lines) {
		t.Fatalf("scalars.golden has %d lines for %d cases; regenerate it", len(want), len(lines))
	}
	for i, line := range lines {
		got := "ERROR"
		if v, err := Parse([]byte("v: " + line)); err == nil {
			got = Canon(v)
		}
		if exp := strings.SplitN(want[i], "\t", 2)[1]; got != exp {
			t.Errorf("%q: got %s, PyYAML gives %s", line, got, exp)
		}
	}
}

// PyYAML: {'b': 2, 'c': 3, 'a': 0}. Earlier merged maps win, own keys win over both.
func TestMergeKeysLetOwnKeysWin(t *testing.T) {
	v, err := Parse([]byte("base: &b {a: 1, b: 2}\nother: &o {b: 9, c: 3}\nx:\n  <<: [*b, *o]\n  a: 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	x, _ := v.Get("x")
	if got := Canon(x); got != `{str:"b"=int:2,str:"c"=int:3,str:"a"=int:0}` {
		t.Errorf("got %s", got)
	}
}

func TestDuplicateKeyLastWins(t *testing.T) {
	v, err := Parse([]byte("a: 1\na: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := Canon(v); got != `{str:"a"=int:2}` {
		t.Errorf("got %s", got)
	}
}

func TestTrueEqualsOne(t *testing.T) {
	if !Equal(Value{Kind: Bool, B: true}, Value{Kind: Int, I: 1}) {
		t.Error("True == 1 in Python")
	}
	if Equal(NewStr("1"), Value{Kind: Int, I: 1}) {
		t.Error(`"1" != 1 in Python`)
	}
}
