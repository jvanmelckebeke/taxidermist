// Package check validates frontmatter against a schema.
//
// Six kinds of fault:
//
//	value    a value outside the field's allowed values for that doctype, a reference
//	         naming no page, or a field on a doctype it does not apply to
//	type     a value of the wrong type
//	missing  a required field absent
//	unknown  a key in neither the required nor the optional list: the one-off key
//	         nobody else uses, which is how a vocabulary forks
//	shape    an object field, or an item of a list of objects, whose keys are wrong
//	parse    a block that opens and does not parse, so the file is invisible to
//	         every reader while looking correct on disk
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

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
	"github.com/jvanmelckebeke/taxidermist/internal/pyyaml"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

var Kinds = []string{"value", "type", "missing", "unknown", "shape", "parse"}

type Fault struct {
	Path  string // absolute
	Kind  string
	Field string
	Value *pyyaml.Value // nil when there is no value to show
	Why   string
}

type Result struct {
	Files      []string            // every file handed to the check, absolute
	Faults     []Fault             // in file order
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

func (c *Checker) file(path string, r *Result) {
	add := func(kind, field string, v *pyyaml.Value, why string) {
		if v != nil && v.Kind == pyyaml.Null {
			v = nil
		}
		r.Faults = append(r.Faults, Fault{Path: path, Kind: kind, Field: field, Value: v, Why: why})
	}
	fm, err := frontmatter.Read(path)
	if err != nil {
		add("parse", "<frontmatter>", nil, "the block opens and does not parse, so the file is invisible: "+firstLine(err.Error()))
		return
	}
	if fm.Kind != pyyaml.Map || len(fm.Keys) == 0 {
		return
	}
	dt, ok := fm.Get("type")
	if !ok || dt.Kind == pyyaml.Null {
		add("missing", "type", nil, "every governed document declares its type")
		return
	}
	t := c.S.Types[dt.String()]
	if t == nil || dt.Kind != pyyaml.Str {
		if c.S.Ungoverned == "report" {
			r.Ungoverned[dt.String()] = append(r.Ungoverned[dt.String()], path)
			return
		}
		add("value", "type", &dt, "no types/ file declares this doctype. Known: "+strings.Join(c.S.Doctypes(), " · "))
		return
	}

	required := append([]string(nil), t.Required...)
	optional := append([]string(nil), t.Optional...)
	for _, cond := range t.RequiredWhen {
		all := true
		for _, w := range cond.When {
			got, _ := fm.Get(w[0].String())
			if !pyyaml.Equal(got, w[1]) {
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
		if !fm.Has(f) && !skip[f] {
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
	for i, k := range fm.Keys {
		v := fm.Vals[i]
		name := k.String()
		if k.Kind != pyyaml.Str || !allowed[name] {
			add("unknown", name, &v, "not declared for `"+t.Doctype+"` in types/"+t.Slug+".yaml")
			continue
		}
		spec := t.Resolved[name]
		if spec == nil {
			if len(c.S.Owners[name]) > 0 {
				add("value", name, &v, "`"+name+"` does not apply to `"+t.Doctype+"`")
			}
			continue
		}
		c.field(name, v, spec, add)
	}
}

type adder func(kind, field string, v *pyyaml.Value, why string)

func (c *Checker) field(name string, v pyyaml.Value, spec *schema.Field, add adder) {
	if !TypeOK(v, spec.Value) {
		add("type", name, &v, "expected "+spec.Value+", got "+v.TypeName())
		return
	}
	if spec.Value == "object" {
		c.keys(name, v, spec.Schema, add)
		return
	}
	if spec.Value == "list" && len(spec.Items) > 0 {
		for i, item := range v.Items {
			label := name + "[" + itoa(i) + "]"
			if item.Kind != pyyaml.Map {
				add("shape", label, &item, "expected a mapping with "+strings.Join(sortedNames(spec.Items), " · ")+", got "+item.TypeName())
				continue
			}
			c.keys(label, item, spec.Items, add)
		}
		return
	}
	if spec.Reference != "" {
		c.references(name, v, spec.Reference, add)
		return
	}
	enum := spec.Enum(c.S)
	if len(enum) == 0 {
		return
	}
	for _, x := range each(v) {
		if x.Kind != pyyaml.Null && !member(x, enum) {
			add("value", name, &x, "allowed: "+strings.Join(sortedKeys(enum), " · "))
		}
	}
}

// keys checks one mapping against a `schema:` or `items:` block: declared keys typed,
// nothing else.
func (c *Checker) keys(label string, v pyyaml.Value, sub []*schema.Field, add adder) {
	known := map[string]bool{}
	for _, rule := range sub {
		known[rule.Name] = true
		got, ok := v.Get(rule.Name)
		if !ok {
			if rule.Required {
				add("missing", label+"."+rule.Name, nil, "required key of `"+label+"`")
			}
			continue
		}
		key := label + "." + rule.Name
		if !TypeOK(got, rule.Value) {
			add("type", key, &got, "expected "+rule.Value+", got "+got.TypeName())
			continue
		}
		if enum := rule.Enum(c.S); len(enum) > 0 && !member(got, enum) {
			var keys []string
			for _, d := range enum {
				keys = append(keys, d.Key.String())
			}
			add("value", key, &got, "not a defined value. Allowed: "+strings.Join(keys, " · "))
			continue
		}
		if rule.Reference != "" {
			c.references(key, got, rule.Reference, add)
		}
	}
	for i, k := range v.Keys {
		if k.Kind != pyyaml.Str || !known[k.S] {
			val := v.Vals[i]
			add("shape", label+"."+k.String(), &val, "not a key of `"+label+"`. Allowed: "+strings.Join(sortedNames(sub), " · "))
		}
	}
}

// references checks that every value is the slug of a page under the base directory
// the field names: the allowed values are whatever pages exist.
func (c *Checker) references(label string, v pyyaml.Value, dir string, add adder) {
	pages := c.pages(dir)
	shown := schema.Display(filepath.Join(c.S.Base, dir))
	for _, x := range each(v) {
		if x.Kind == pyyaml.Null || pages[x.String()] {
			continue
		}
		why := "no page at " + shown + "/" + x.String() + ".md"
		if len(pages) > 0 {
			why += ". " + shown + "/ has " + itoa(len(pages)) + " pages"
		} else {
			why += ". " + shown + "/ holds no pages"
		}
		add("value", label, &x, why)
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
// spec checks nothing. A bool never passes as an int or a float.
func TypeOK(v pyyaml.Value, spec string) bool {
	if spec == "" {
		return true
	}
	for _, alt := range strings.Split(spec, "|") {
		switch strings.TrimSpace(alt) {
		case "str":
			if v.Kind == pyyaml.Str {
				return true
			}
		case "list":
			if v.Kind == pyyaml.List {
				return true
			}
		case "bool":
			if v.Kind == pyyaml.Bool {
				return true
			}
		case "object":
			if v.Kind == pyyaml.Map {
				return true
			}
		case "date":
			if v.Kind == pyyaml.Date || v.Kind == pyyaml.Datetime {
				return true
			}
		case "int":
			if v.Kind == pyyaml.Int {
				return true
			}
		case "float":
			if v.Kind == pyyaml.Int || v.Kind == pyyaml.Float {
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

func each(v pyyaml.Value) []pyyaml.Value {
	if v.Kind == pyyaml.List {
		return v.Items
	}
	return []pyyaml.Value{v}
}

func member(v pyyaml.Value, enum []schema.Def) bool {
	if v.Kind == pyyaml.List || v.Kind == pyyaml.Map {
		return false
	}
	for _, d := range enum {
		if pyyaml.Equal(v, d.Key) {
			return true
		}
	}
	return false
}

func sortedKeys(enum []schema.Def) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range enum {
		k := d.Key.String()
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
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

func itoa(i int) string { return strconv.Itoa(i) }
