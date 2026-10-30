// Package check validates frontmatter, and YAML files a doctype's `files:` names,
// against a schema.
//
// Seven kinds of fault:
//
//	value    a value outside the field's allowed values for that doctype, one that
//	         misses its pattern, a reference naming no page or key, a YAML file no
//	         doctype's `files:` matches, or two do, a field on a doctype it does not apply to, a value
//	         that repeats the field its spec `excludes`, a page outside the folder
//	         its doctype's `directory:` names, or a page whose slug another page
//	         under that folder already uses
//	type     a value of the wrong type
//	missing  a required field absent
//	unknown  a key in neither the required nor the optional list: the one-off key
//	         nobody else uses, which is how a vocabulary forks
//	shape    an object field, or an item of a list of objects, whose keys are wrong
//	parse    a block that opens and does not parse, so the file is invisible to
//	         every reader while looking correct on disk, or a YAML file that does
//	         not parse
//	segment  a folder name, or the stem of a page directly under a segment rule's
//	         directory, that is not one of the rule's values
//
// One kind of warning, which never fails a run:
//
//	mismatch a file whose segment rule names a field, and whose value for that field
//	         differs from the folder it sits in
//
// There is no union fallback. A field belongs to the doctypes that declare it, through
// their own type file, a trait they compose, or a singleton's `applies_to`, and is an
// error anywhere else.
package check

import (
	"os"
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

var Kinds = []string{"value", "type", "missing", "unknown", "shape", "parse", "segment"}

type Fault struct {
	Path  string // absolute
	Kind  string
	Field string
	Value any // nil when there is no value to show
	Why   string
	Dir   string // a segment fault or mismatch warning: the folder the segment names, absolute
}

type Result struct {
	Files      []string            // every file handed to the check, absolute
	Faults     []Fault             // in file order
	Warnings   []Fault             // in file order; never fail a run
	Ungoverned map[string][]string // doctype -> files, when scope.yaml says `ungoverned: report`
}

// Governed is the number of checked files whose doctype has a schema, or that had
// no doctype to look up.
func (r *Result) Governed() int {
	n := len(r.Files)
	for _, fs := range r.Ungoverned {
		n -= len(fs)
	}
	return n
}

type Checker struct {
	S     *schema.Schema
	refs  map[string]map[string]bool
	stems map[string]map[string][]string // a directory -> page stem -> every page with it, recursively
}

func New(s *schema.Schema) *Checker {
	return &Checker{S: s, refs: map[string]map[string]bool{}, stems: map[string]map[string][]string{}}
}

// Run checks each file.
func (c *Checker) Run(files []string) *Result {
	r := &Result{Files: files, Ungoverned: map[string][]string{}}
	for _, p := range files {
		raw, _ := os.ReadFile(p)
		c.file(p, raw, r)
	}
	return r
}

// Content checks raw as the file at path, which is absolute and need not exist.
func (c *Checker) Content(path string, raw []byte) *Result {
	r := &Result{Files: []string{path}, Ungoverned: map[string][]string{}}
	c.file(path, raw, r)
	return r
}

type adder func(kind, field string, v any, why string)

func each(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return []any{v}
}

// member compares a scalar's YAML text with the defined values, so `1`, `true` and
// `2026-04-01` match keys written the same way.
func member(v any, enum []schema.Def) bool {
	if !value.IsScalar(v) {
		return false
	}
	t := value.Text(v)
	for _, d := range enum {
		if d.Key == t {
			return true
		}
	}
	return false
}

func sortedKeys(enum []schema.Def) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range enum {
		if !seen[d.Key] {
			seen[d.Key] = true
			out = append(out, d.Key)
		}
	}
	sort.Strings(out)
	return out
}

func sortedNames(fs []*schema.Field) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	sort.Strings(out)
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
