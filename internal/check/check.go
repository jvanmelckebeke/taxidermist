// Package check validates frontmatter against a schema.
//
// Seven kinds of fault:
//
//	value    a value outside the field's allowed values for that doctype, a reference
//	         naming no page, a field on a doctype it does not apply to, a value
//	         that repeats the field its spec `excludes`, a page outside the folder
//	         its doctype's `directory:` names, or a page whose slug another page
//	         under that folder already uses
//	type     a value of the wrong type
//	missing  a required field absent
//	unknown  a key in neither the required nor the optional list: the one-off key
//	         nobody else uses, which is how a vocabulary forks
//	shape    an object field, or an item of a list of objects, whose keys are wrong
//	parse    a block that opens and does not parse, so the file is invisible to
//	         every reader while looking correct on disk
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
	"path/filepath"
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
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
		c.file(p, r)
	}
	return r
}

type adder func(kind, field string, v any, why string)

func (c *Checker) file(path string, r *Result) {
	add := func(kind, field string, v any, why string) {
		r.Faults = append(r.Faults, Fault{Path: path, Kind: kind, Field: field, Value: v, Why: why})
	}
	segs := c.segments(path)
	for _, sg := range segs {
		if why := c.segmentFault(sg.rule, sg.name); why != "" {
			r.Faults = append(r.Faults, Fault{Path: path, Kind: "segment", Field: sg.rule.Path, Value: sg.name, Why: why, Dir: sg.dir(c.S)})
		}
	}
	fm, err := frontmatter.Read(path)
	if err == nil && fm != nil {
		for _, sg := range segs {
			if sg.rule.Field == "" {
				continue
			}
			v, ok := fm.Get(sg.rule.Field)
			if !ok || hasText(v, sg.name) {
				continue
			}
			r.Warnings = append(r.Warnings, Fault{Path: path, Kind: "mismatch", Field: sg.rule.Field, Value: v,
				Why: "the folder says `" + sg.name + "` (" + sg.rule.Path + ")", Dir: sg.dir(c.S)})
		}
	}
	if err != nil {
		add("parse", "<frontmatter>", nil, "the block opens and does not parse, so the file is invisible: "+firstLine(err.Error()))
		return
	}
	if fm == nil || len(fm.Keys) == 0 {
		return
	}
	dt, _ := fm.Get("type")
	if dt == nil {
		add("missing", "type", nil, "every governed document declares its type")
		return
	}
	name, isStr := dt.(string)
	t := c.S.Types[name]
	if t == nil || !isStr {
		if c.S.Ungoverned == "report" {
			key := value.Text(dt)
			r.Ungoverned[key] = append(r.Ungoverned[key], path)
			return
		}
		add("value", "type", dt, "no types/ file declares this doctype. Known: "+strings.Join(c.S.Doctypes(), " · "))
		return
	}

	required := append([]string(nil), t.Required...)
	optional := append([]string(nil), t.Optional...)
	for _, cond := range t.RequiredWhen {
		all := true
		for i, k := range cond.When.Keys {
			got, _ := fm.Get(k)
			if value.Text(got) != value.Text(cond.When.Vals[i]) {
				all = false
				break
			}
		}
		if all {
			required = append(required, cond.Field)
		} else if !contains(optional, cond.Field) {
			optional = append(optional, cond.Field)
		}
	}

	skip := c.exemptions(path)
	for _, f := range required {
		if _, ok := fm.Get(f); !ok && !skip[f] {
			add("missing", f, nil, "required on `"+t.Doctype+"`")
		}
	}
	if t.Directory != "" {
		c.directory(path, fm, t.Directory, required, add)
	}

	allowed := map[string]bool{}
	for _, f := range required {
		allowed[f] = true
	}
	for _, f := range optional {
		allowed[f] = true
	}
	for _, name := range fm.Keys {
		v := fm.Fields[name]
		if !allowed[name] {
			add("unknown", name, v, "not declared for `"+t.Doctype+"` in types/"+t.Slug+".yaml")
			continue
		}
		spec := t.Resolved[name]
		if spec == nil {
			if len(c.S.Owners[name]) > 0 {
				add("value", name, v, "`"+name+"` does not apply to `"+t.Doctype+"`")
			}
			continue
		}
		c.field(name, v, spec, add)
		if spec.Excludes == "" {
			continue
		}
		other, ok := fm.Get(spec.Excludes)
		if !ok {
			continue
		}
		for _, x := range each(v) {
			for _, y := range each(other) {
				if x != nil && value.Text(x) == value.Text(y) {
					add("value", name, x, "repeats the file's own `"+spec.Excludes+"`")
				}
			}
		}
	}
}

func (c *Checker) exemptions(path string) map[string]bool {
	out := map[string]bool{}
	rel, err := filepath.Rel(c.S.Base, path)
	if err != nil {
		return out
	}
	rel = filepath.ToSlash(rel)
	for _, e := range c.S.Exempt {
		if Fnmatch(rel, e.Paths) {
			for _, f := range e.Omit {
				out[f] = true
			}
		}
	}
	return out
}

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
