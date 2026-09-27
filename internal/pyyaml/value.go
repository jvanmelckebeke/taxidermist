// Package pyyaml parses YAML into the values PyYAML's safe_load would produce.
//
// Frontmatter is read by Python tools in the repos this checks, and PyYAML
// follows YAML 1.1: `2026-04-01` is a date, `yes` is a bool, `1e5` is a string.
// Go YAML libraries follow 1.2. A checker that types values differently from
// the reader passes files the reader then sees differently, so plain scalars
// are resolved here with PyYAML's own resolver patterns.
package pyyaml

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type Kind int

const (
	Null Kind = iota
	Bool
	Int
	Float
	Str
	Date
	Datetime
	List
	Map
)

// Value is one parsed node. Maps keep insertion order, as a Python dict does.
type Value struct {
	Kind  Kind
	B     bool
	I     int64
	Big   string // decimal text of an int that overflows int64
	F     float64
	S     string
	T     time.Time
	HasTZ bool
	Items []Value
	Keys  []Value
	Vals  []Value
}

func NewStr(s string) Value { return Value{Kind: Str, S: s} }

// Get returns the value under a string key. Non-string keys never match,
// as `fm.get("1")` does not find the int key 1 in Python.
func (v Value) Get(key string) (Value, bool) {
	if v.Kind != Map {
		return Value{}, false
	}
	for i, k := range v.Keys {
		if k.Kind == Str && k.S == key {
			return v.Vals[i], true
		}
	}
	return Value{}, false
}

// Has reports whether a string key is present.
func (v Value) Has(key string) bool {
	_, ok := v.Get(key)
	return ok
}

// Set assigns a key, replacing an equal one in place (a later duplicate
// key wins, as in PyYAML).
func (v *Value) Set(key, val Value) {
	for i, k := range v.Keys {
		if Equal(k, key) {
			v.Vals[i] = val
			return
		}
	}
	v.Keys = append(v.Keys, key)
	v.Vals = append(v.Vals, val)
}

// TypeName is the name Python's checker printed for a value's type.
func (v Value) TypeName() string {
	switch v.Kind {
	case Null:
		return "NoneType"
	case Bool:
		return "bool"
	case Int:
		return "int"
	case Float:
		return "float"
	case Str:
		return "str"
	case Date:
		return "date"
	case Datetime:
		return "datetime"
	case List:
		return "list"
	case Map:
		return "object"
	}
	return "unknown"
}

func (v Value) numeric() (float64, bool) {
	switch v.Kind {
	case Bool:
		if v.B {
			return 1, true
		}
		return 0, true
	case Int:
		if v.Big != "" {
			f, _ := strconv.ParseFloat(v.Big, 64)
			return f, true
		}
		return float64(v.I), true
	case Float:
		return v.F, true
	}
	return 0, false
}

// Equal is Python's `==` for the values frontmatter holds: True == 1 == 1.0,
// a str never equals a number, and containers compare element-wise.
func Equal(a, b Value) bool {
	if fa, ok := a.numeric(); ok {
		fb, ok := b.numeric()
		if !ok {
			return false
		}
		if a.Kind == Int && b.Kind == Int && (a.Big != "" || b.Big != "") {
			return a.Big == b.Big && a.I == b.I
		}
		return fa == fb
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case Null:
		return true
	case Str:
		return a.S == b.S
	case Date:
		return a.T.Equal(b.T)
	case Datetime:
		return a.HasTZ == b.HasTZ && a.T.Equal(b.T)
	case List:
		if len(a.Items) != len(b.Items) {
			return false
		}
		for i := range a.Items {
			if !Equal(a.Items[i], b.Items[i]) {
				return false
			}
		}
		return true
	case Map:
		if len(a.Keys) != len(b.Keys) {
			return false
		}
		for i, k := range a.Keys {
			found := false
			for j, k2 := range b.Keys {
				if Equal(k, k2) {
					found = Equal(a.Vals[i], b.Vals[j])
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return false
}

// String is Python's str(value).
func (v Value) String() string {
	switch v.Kind {
	case Null:
		return "None"
	case Bool:
		if v.B {
			return "True"
		}
		return "False"
	case Int:
		if v.Big != "" {
			return v.Big
		}
		return strconv.FormatInt(v.I, 10)
	case Float:
		return pyFloat(v.F)
	case Str:
		return v.S
	case Date:
		return v.T.Format("2006-01-02")
	case Datetime:
		s := v.T.Format("2006-01-02 15:04:05")
		if us := v.T.Nanosecond() / 1000; us != 0 {
			s += fmt.Sprintf(".%06d", us)
		}
		if v.HasTZ {
			_, off := v.T.Zone()
			sign := "+"
			if off < 0 {
				sign, off = "-", -off
			}
			s += fmt.Sprintf("%s%02d:%02d", sign, off/3600, off%3600/60)
		}
		return s
	case List:
		parts := make([]string, len(v.Items))
		for i, it := range v.Items {
			parts[i] = it.Repr()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case Map:
		parts := make([]string, len(v.Keys))
		for i := range v.Keys {
			parts[i] = v.Keys[i].Repr() + ": " + v.Vals[i].Repr()
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return ""
}

// Repr is Python's repr(value), close enough for messages.
func (v Value) Repr() string {
	switch v.Kind {
	case Str:
		return PyRepr(v.S)
	case Date:
		return fmt.Sprintf("datetime.date(%d, %d, %d)", v.T.Year(), int(v.T.Month()), v.T.Day())
	case Datetime:
		return "datetime.datetime(" + v.String() + ")"
	}
	return v.String()
}

// PyRepr quotes a string the way Python's repr does.
func PyRepr(s string) string {
	q := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		q = '"'
	}
	var b strings.Builder
	b.WriteByte(q)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == rune(q):
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte(q)
	return b.String()
}

func pyFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	}
	// Python's repr: shortest digits, positional for exponents -4..15.
	e := strconv.FormatFloat(f, 'e', -1, 64)
	mant, exp, _ := strings.Cut(e, "e")
	n, _ := strconv.Atoi(exp)
	if n >= -4 && n < 16 {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	sign := "+"
	if n < 0 {
		sign, n = "-", -n
	}
	return fmt.Sprintf("%se%s%02d", mant, sign, n)
}

// Truncate cuts a string to n code points, as Python's s[:n] does.
func Truncate(s string, n int) string {
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}
