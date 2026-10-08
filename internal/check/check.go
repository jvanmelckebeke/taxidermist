// Package check validates frontmatter against a schema.
//
// Seven kinds of fault:
//
//	value    a value outside the field's allowed values for that doctype, a reference
//	         naming no page, a field on a doctype it does not apply to, or a value
//	         that repeats the field its spec `excludes`
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
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
	S    *schema.Schema
	refs map[string]map[string]bool
}

func New(s *schema.Schema) *Checker {
	return &Checker{S: s, refs: map[string]map[string]bool{}}
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

// placed is a file's place under one segment rule: the name it sits under.
type placed struct {
	rule *schema.Segment
	name string
}

func (p placed) dir(s *schema.Schema) string {
	return filepath.Join(s.Base, filepath.FromSlash(p.rule.Dir), p.name)
}

// segments returns, for each rule whose directory holds the file, the first path
// component below that directory: a folder name, or the stem of a page sitting
// directly there. index.md and README.md directly in the directory describe the
// directory itself and name no segment.
func (c *Checker) segments(path string) []placed {
	rel, err := filepath.Rel(c.S.Base, path)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	var out []placed
	for _, rule := range c.S.Segments {
		rest, ok := strings.CutPrefix(rel, rule.Dir+"/")
		if !ok {
			continue
		}
		name, _, nested := strings.Cut(rest, "/")
		if !nested {
			name = strings.TrimSuffix(name, ".md")
			if name == "index" || name == "README" {
				continue
			}
		}
		out = append(out, placed{rule, name})
	}
	return out
}

func (c *Checker) segmentFault(rule *schema.Segment, name string) string {
	if rule.Pattern != nil {
		if rule.Pattern.MatchString(name) {
			return ""
		}
		return "the name does not match `" + rule.Pattern.String() + "`"
	}
	for _, d := range rule.Values {
		if d.Key == name {
			return ""
		}
	}
	return "not a value of `" + rule.Vocabulary + "`, so the folder names nothing the vocabulary defines. Allowed: " +
		strings.Join(sortedKeys(rule.Values), " · ")
}

// hasText is true when v, or any item of a list v, is written as want.
func hasText(v any, want string) bool {
	for _, x := range each(v) {
		if value.IsScalar(x) && value.Text(x) == want {
			return true
		}
	}
	return false
}

func (c *Checker) field(name string, v any, spec *schema.Field, add adder) {
	if !TypeOK(v, spec.Value) {
		add("type", name, v, "expected "+spec.Value+", got "+value.TypeName(v))
		return
	}
	if spec.Value == "object" {
		c.keys(name, asMap(v), spec.Schema.Vals, add)
		return
	}
	if spec.Value == "list" && len(spec.Items.Vals) > 0 {
		for i, item := range v.([]any) {
			label := name + "[" + strconv.Itoa(i) + "]"
			m, ok := asMapOK(item)
			if !ok {
				add("shape", label, item, "expected a mapping with "+strings.Join(sortedNames(spec.Items.Vals), " · ")+", got "+value.TypeName(item))
				continue
			}
			c.keys(label, m, spec.Items.Vals, add)
		}
		return
	}
	if spec.Reference != "" {
		c.references(name, v, spec.Reference, add)
		return
	}
	enum := spec.Enum()
	if len(enum) == 0 {
		return
	}
	for _, x := range each(v) {
		if x != nil && !member(x, enum) {
			add("value", name, x, "allowed: "+strings.Join(sortedKeys(enum), " · "))
		}
	}
}

// keys checks one mapping against a `schema:` or `items:` block: declared keys typed,
// nothing else.
func (c *Checker) keys(label string, m map[string]any, sub []*schema.Field, add adder) {
	known := map[string]bool{}
	for _, rule := range sub {
		known[rule.Name] = true
		got, ok := m[rule.Name]
		key := label + "." + rule.Name
		if !ok {
			if rule.Required {
				add("missing", key, nil, "required key of `"+label+"`")
			}
			continue
		}
		if !TypeOK(got, rule.Value) {
			add("type", key, got, "expected "+rule.Value+", got "+value.TypeName(got))
			continue
		}
		if enum := rule.Enum(); len(enum) > 0 && !member(got, enum) {
			var keys []string
			for _, d := range enum {
				keys = append(keys, d.Key)
			}
			add("value", key, got, "not a defined value. Allowed: "+strings.Join(keys, " · "))
			continue
		}
		if rule.Reference != "" {
			c.references(key, got, rule.Reference, add)
		}
	}
	extra := make([]string, 0)
	for k := range m {
		if !known[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		add("shape", label+"."+k, m[k], "not a key of `"+label+"`. Allowed: "+strings.Join(sortedNames(sub), " · "))
	}
}

func asMapOK(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, x := range m {
			out[value.Text(k)] = x
		}
		return out, true
	}
	return nil, false
}

func asMap(v any) map[string]any {
	m, _ := asMapOK(v)
	return m
}

// references checks that every value is the slug of a page under the base directory
// the field names: the allowed values are whatever pages exist.
func (c *Checker) references(label string, v any, dir string, add adder) {
	pages := c.pages(dir)
	shown := schema.Display(filepath.Join(c.S.Base, dir))
	for _, x := range each(v) {
		if x == nil || pages[value.Text(x)] {
			continue
		}
		why := "no page at " + shown + "/" + value.Text(x) + ".md"
		if len(pages) > 0 {
			why += ". " + shown + "/ has " + strconv.Itoa(len(pages)) + " pages"
		} else {
			why += ". " + shown + "/ holds no pages"
		}
		add("value", label, x, why)
	}
}

func (c *Checker) pages(dir string) map[string]bool {
	if p, ok := c.refs[dir]; ok {
		return p
	}
	out := map[string]bool{}
	matches, _ := filepath.Glob(filepath.Join(c.S.Base, dir, "*.md"))
	for _, m := range matches {
		stem := strings.TrimSuffix(filepath.Base(m), ".md")
		if stem != "index" {
			out[stem] = true
		}
	}
	c.refs[dir] = out
	return out
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

// TypeOK checks a value against a `value:` spec, possibly an `a|b` union. An empty
// spec checks nothing.
func TypeOK(v any, spec string) bool {
	if spec == "" {
		return true
	}
	for _, alt := range strings.Split(spec, "|") {
		switch strings.TrimSpace(alt) {
		case "str":
			if _, ok := v.(string); ok {
				return true
			}
		case "list":
			if _, ok := v.([]any); ok {
				return true
			}
		case "bool":
			if _, ok := v.(bool); ok {
				return true
			}
		case "object":
			if _, ok := asMapOK(v); ok {
				return true
			}
		case "date":
			if _, ok := v.(time.Time); ok {
				return true
			}
		case "int":
			switch v.(type) {
			case int, int64, uint64:
				return true
			}
		case "float":
			switch v.(type) {
			case int, int64, uint64, float64:
				return true
			}
		}
	}
	return false
}

// Files returns the files to check, always filtered to the governed roots. With no
// arguments it walks every root. Arguments are filtered the same way, so a caller
// never needs its own copy of the root list: a hook hands over every staged markdown
// file and scope.yaml alone decides which of them the schema governs.
func Files(s *schema.Schema, args []string) ([]string, error) {
	roots, err := s.GovernedRoots()
	if err != nil {
		return nil, err
	}
	if len(args) > 0 {
		var out []string
		for _, a := range args {
			if !strings.HasSuffix(a, ".md") {
				continue
			}
			p, err := filepath.Abs(a)
			if err != nil {
				continue
			}
			if st, err := os.Stat(p); err != nil || st.IsDir() {
				continue
			}
			for _, r := range roots {
				if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
					out = append(out, p)
					break
				}
			}
		}
		return out, nil
	}
	var out []string
	for _, r := range roots {
		real, err := filepath.EvalSymlinks(r)
		if err != nil {
			return nil, err
		}
		err = filepath.WalkDir(real, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				if st, err := os.Stat(p); err != nil || st.IsDir() {
					return nil
				}
			}
			rel, _ := filepath.Rel(real, p)
			out = append(out, filepath.Join(r, rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sortPaths(out)
	return out, nil
}

// sortPaths orders paths component by component, as Python sorts Path objects.
func sortPaths(ps []string) {
	sort.SliceStable(ps, func(i, j int) bool {
		a := strings.Split(ps[i], string(filepath.Separator))
		b := strings.Split(ps[j], string(filepath.Separator))
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
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
