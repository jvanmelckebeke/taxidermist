// Package schema loads a taxonomy directory.
//
//	scope.yaml           the roots the schema governs, and how it treats doctypes it has no file for
//	exempt.yaml          declared grandfathering: a path glob and the fields it may omit
//	traits.yaml          field bundles a doctype composes
//	singletons.yaml      shared fields that form no bundle, each naming its doctypes
//	vocabularies/*.yaml  value lists shared by fields on several doctypes
//	types/<slug>.yaml    one file per doctype: its traits, field set and own fields
//
// A field's `definitions` keys are its allowed values. There is no second list, so a
// value cannot exist until someone has written down what it means.
package schema

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/pyyaml"
)

// Def is one allowed value and what it means.
type Def struct {
	Key   pyyaml.Value
	Means string
	Group string // the vocabulary group it sits under, if any
}

// Field is a field spec, or a key of an object field's `schema:` or a list's `items:`.
type Field struct {
	Name        string
	Value       string // a type name or an `a|b` union; empty means unchecked
	Guidance    string
	Definitions []Def
	Vocabulary  string
	Dynamic     bool
	Schema      []*Field // keys of a `value: object`
	Items       []*Field // keys of each mapping in a `value: list`
	Reference   string   // directory, relative to the base, whose page slugs are the values
	Required    bool     // for nested keys only
	AppliesTo   []string // for singletons only
	Raw         pyyaml.Value
}

// Enum is the field's allowed values: its definitions, or its vocabulary's.
// Nil when the field is free, dynamic, or a reference.
func (f *Field) Enum(s *Schema) []Def {
	if f.Dynamic {
		return nil
	}
	if f.Vocabulary != "" {
		return s.Vocabularies[f.Vocabulary].Defs
	}
	return f.Definitions
}

// Condition makes a field required when every listed field has the listed value.
type Condition struct {
	Field string
	When  [][2]pyyaml.Value // field name (as Str) and value
}

// Type is one doctype.
type Type struct {
	Doctype      string // the literal `type:` value
	Slug         string // the file stem, used for the format page name
	Traits       []string
	Required     []string
	Optional     []string
	RequiredWhen []Condition
	Fields       []*Field          // declared in this file
	Resolved     map[string]*Field // own fields, then singletons, then traits
}

// Declared is every field the doctype may carry.
func (t *Type) Declared() map[string]bool {
	out := map[string]bool{}
	for _, n := range t.Required {
		out[n] = true
	}
	for _, n := range t.Optional {
		out[n] = true
	}
	return out
}

type Trait struct {
	Name   string
	Fields []*Field
}

type Vocabulary struct {
	Name string
	Defs []Def
}

type Exemption struct {
	Paths string
	Omit  []string
	Why   string
}

type Schema struct {
	Dir          string // the taxonomy directory
	Base         string // its parent: roots, references and exemptions resolve against it
	Roots        []string
	Ungoverned   string // "error" or "report"
	Types        map[string]*Type
	Traits       []*Trait
	Singletons   []*Field
	Vocabularies map[string]*Vocabulary
	Exempt       []Exemption
	Owners       map[string]map[string]bool // field -> doctypes that declare it
}

// Doctypes returns the doctype names, sorted.
func (s *Schema) Doctypes() []string {
	out := make([]string, 0, len(s.Types))
	for d := range s.Types {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func (s *Schema) Trait(name string) *Trait {
	for _, t := range s.Traits {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// Error is a fault in the schema itself; nothing can be checked until it is fixed.
type Error struct{ Msg string }

func (e *Error) Error() string { return "taxonomy: " + e.Msg }

func errf(format string, a ...any) error { return &Error{fmt.Sprintf(format, a...)} }

// Load reads a taxonomy directory.
func Load(dir string) (*Schema, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return nil, errf("%s is not a directory. Pass --taxonomy DIR.", dir)
	}
	s := &Schema{
		Dir: abs, Base: filepath.Dir(abs),
		Types: map[string]*Type{}, Vocabularies: map[string]*Vocabulary{},
		Owners: map[string]map[string]bool{},
	}

	scope, err := readYAML(filepath.Join(abs, "scope.yaml"))
	if err != nil {
		return nil, err
	}
	s.Roots = strList(get(scope, "roots"))
	s.Ungoverned = "error"
	if u, ok := scope.Get("ungoverned"); ok && u.Kind != pyyaml.Null {
		s.Ungoverned = u.String()
	}
	if s.Ungoverned != "error" && s.Ungoverned != "report" {
		return nil, errf("scope.yaml sets `ungoverned: %s`; it takes `error` or `report`.", s.Ungoverned)
	}

	exempt, err := readYAML(filepath.Join(abs, "exempt.yaml"))
	if err != nil {
		return nil, err
	}
	for i, e := range get(exempt, "exempt").Items {
		p, ok := e.Get("paths")
		if !ok {
			return nil, errf("exempt.yaml entry %d has no `paths:` glob.", i+1)
		}
		s.Exempt = append(s.Exempt, Exemption{Paths: p.String(), Omit: strList(get(e, "omit")), Why: get(e, "why").String()})
	}

	if err := s.loadVocabularies(); err != nil {
		return nil, err
	}

	traits, err := readYAML(filepath.Join(abs, "traits.yaml"))
	if err != nil {
		return nil, err
	}
	tm := get(traits, "traits")
	for i, k := range tm.Keys {
		t := &Trait{Name: k.String()}
		fm := tm.Vals[i]
		for j, fk := range fm.Keys {
			f, err := s.parseField(fk.String(), fm.Vals[j], "traits.yaml")
			if err != nil {
				return nil, err
			}
			t.Fields = append(t.Fields, f)
		}
		s.Traits = append(s.Traits, t)
	}

	singles, err := readYAML(filepath.Join(abs, "singletons.yaml"))
	if err != nil {
		return nil, err
	}
	sm := get(singles, "fields")
	for i, k := range sm.Keys {
		f, err := s.parseField(k.String(), sm.Vals[i], "singletons.yaml")
		if err != nil {
			return nil, err
		}
		f.AppliesTo = strList(get(sm.Vals[i], "applies_to"))
		s.Singletons = append(s.Singletons, f)
	}

	files, _ := filepath.Glob(filepath.Join(abs, "types", "*.yaml"))
	sort.Strings(files)
	for _, file := range files {
		if err := s.loadType(file); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Schema) loadVocabularies() error {
	files, _ := filepath.Glob(filepath.Join(s.Dir, "vocabularies", "*.yaml"))
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".yaml")
		v, err := readYAML(file)
		if err != nil {
			return err
		}
		defs, ok := v.Get("definitions")
		if !ok || defs.Kind != pyyaml.Map {
			return errf("vocabularies/%s.yaml has no `definitions:` mapping.", name)
		}
		voc := &Vocabulary{Name: name}
		for i, k := range defs.Keys {
			val := defs.Vals[i]
			if val.Kind == pyyaml.Map {
				for j, gk := range val.Keys {
					voc.Defs = append(voc.Defs, Def{Key: gk, Means: val.Vals[j].String(), Group: k.String()})
				}
				continue
			}
			voc.Defs = append(voc.Defs, Def{Key: k, Means: meaning(val)})
		}
		s.Vocabularies[name] = voc
	}
	return nil
}

func meaning(v pyyaml.Value) string {
	if v.Kind == pyyaml.Null {
		return ""
	}
	return v.String()
}

func (s *Schema) parseField(name string, v pyyaml.Value, where string) (*Field, error) {
	f := &Field{Name: name, Raw: v}
	if v.Kind != pyyaml.Map {
		return nil, errf("%s: field `%s` is not a mapping.", where, name)
	}
	if x, ok := v.Get("value"); ok && x.Kind != pyyaml.Null {
		f.Value = x.String()
	}
	if x, ok := v.Get("guidance"); ok && x.Kind != pyyaml.Null {
		f.Guidance = x.String()
	}
	if x, ok := v.Get("reference"); ok && x.Kind != pyyaml.Null {
		f.Reference = x.String()
	}
	if x, ok := v.Get("vocabulary"); ok && x.Kind != pyyaml.Null {
		f.Vocabulary = x.String()
		if _, known := s.Vocabularies[f.Vocabulary]; !known {
			return nil, errf("%s: field `%s` names vocabulary `%s`, and vocabularies/%s.yaml does not exist.",
				where, name, f.Vocabulary, f.Vocabulary)
		}
	}
	f.Dynamic = truthy(get(v, "dynamic"))
	f.Required = truthy(get(v, "required"))
	if defs, ok := v.Get("definitions"); ok && defs.Kind == pyyaml.Map {
		for i, k := range defs.Keys {
			f.Definitions = append(f.Definitions, Def{Key: k, Means: meaning(defs.Vals[i])})
		}
	}
	if f.Vocabulary != "" && len(f.Definitions) > 0 {
		return nil, errf("%s: field `%s` has both `definitions:` and `vocabulary:`. Keep one list.", where, name)
	}
	for _, sub := range []struct {
		key string
		dst *[]*Field
	}{{"schema", &f.Schema}, {"items", &f.Items}} {
		m, ok := v.Get(sub.key)
		if !ok || m.Kind != pyyaml.Map {
			continue
		}
		for i, k := range m.Keys {
			child, err := s.parseField(k.String(), m.Vals[i], where+": "+name)
			if err != nil {
				return nil, err
			}
			*sub.dst = append(*sub.dst, child)
		}
	}
	return f, nil
}

func (s *Schema) loadType(file string) error {
	rel := "types/" + filepath.Base(file)
	v, err := readYAML(file)
	if err != nil {
		return err
	}
	t := &Type{Slug: strings.TrimSuffix(filepath.Base(file), ".yaml"), Resolved: map[string]*Field{}}
	t.Doctype = t.Slug
	if d, ok := v.Get("type"); ok && d.Kind != pyyaml.Null {
		t.Doctype = d.String()
	}
	if other, dup := s.Types[t.Doctype]; dup {
		return errf("types/%s.yaml and %s both declare `type: %s`.", other.Slug, rel, t.Doctype)
	}
	t.Traits = strList(get(v, "traits"))
	t.Required = strList(get(v, "required"))
	t.Optional = strList(get(v, "optional"))
	rw := get(v, "required_when")
	for i, k := range rw.Keys {
		c := Condition{Field: k.String()}
		when := rw.Vals[i]
		for j, wk := range when.Keys {
			c.When = append(c.When, [2]pyyaml.Value{wk, when.Vals[j]})
		}
		t.RequiredWhen = append(t.RequiredWhen, c)
	}
	own := get(v, "fields")
	for i, k := range own.Keys {
		f, err := s.parseField(k.String(), own.Vals[i], rel)
		if err != nil {
			return err
		}
		t.Fields = append(t.Fields, f)
	}

	declared := t.Declared()
	for _, name := range t.Traits {
		tr := s.Trait(name)
		if tr == nil {
			var known []string
			for _, x := range s.Traits {
				known = append(known, x.Name)
			}
			return errf("%s composes unknown trait `%s`. traits.yaml has %s.", rel, name, strings.Join(known, ", "))
		}
		var missing []string
		for _, f := range tr.Fields {
			if !declared[f.Name] {
				missing = append(missing, f.Name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return errf("%s composes trait `%s` but never allows %s. List them in required or optional, "+
				"or drop the trait: a trait list that omits its own fields cannot be trusted to mean what it says.",
				rel, name, strings.Join(missing, ", "))
		}
		for _, f := range tr.Fields {
			t.Resolved[f.Name] = f
			s.own(f.Name, t.Doctype)
		}
	}
	for _, f := range s.Singletons {
		for _, d := range f.AppliesTo {
			if d == t.Doctype {
				t.Resolved[f.Name] = f
				s.own(f.Name, t.Doctype)
			}
		}
	}
	for _, f := range t.Fields {
		t.Resolved[f.Name] = f
		s.own(f.Name, t.Doctype)
	}
	s.Types[t.Doctype] = t
	return nil
}

func (s *Schema) own(field, doctype string) {
	if s.Owners[field] == nil {
		s.Owners[field] = map[string]bool{}
	}
	s.Owners[field][doctype] = true
}

// readYAML reads a schema file. A missing file is an empty mapping.
func readYAML(path string) (pyyaml.Value, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return pyyaml.Value{Kind: pyyaml.Map}, nil
	}
	if err != nil {
		return pyyaml.Value{}, err
	}
	v, err := pyyaml.Parse(raw)
	if err != nil {
		return pyyaml.Value{}, errf("%s does not parse: %v", path, err)
	}
	if v.Kind == pyyaml.Null {
		return pyyaml.Value{Kind: pyyaml.Map}, nil
	}
	if v.Kind != pyyaml.Map {
		return pyyaml.Value{}, errf("%s is not a mapping.", path)
	}
	return v, nil
}

func get(v pyyaml.Value, key string) pyyaml.Value {
	x, _ := v.Get(key)
	return x
}

func strList(v pyyaml.Value) []string {
	var out []string
	for _, it := range v.Items {
		out = append(out, it.String())
	}
	return out
}

// truthy is Python's bool() for the scalars a schema flag holds.
func truthy(v pyyaml.Value) bool {
	switch v.Kind {
	case pyyaml.Null:
		return false
	case pyyaml.Bool:
		return v.B
	case pyyaml.Int:
		return v.I != 0 || v.Big != ""
	case pyyaml.Float:
		return v.F != 0
	case pyyaml.Str:
		return v.S != ""
	case pyyaml.List:
		return len(v.Items) > 0
	case pyyaml.Map:
		return len(v.Keys) > 0
	}
	return true
}

// GovernedRoots returns the absolute roots scope.yaml declares. A listed root that
// does not exist is a fault in scope.yaml, not a reason to quietly check less: a
// typo would otherwise drop a whole area from the gate and every commit would pass.
func (s *Schema) GovernedRoots() ([]string, error) {
	if len(s.Roots) == 0 {
		return nil, errf("scope.yaml declares no roots, so nothing would be checked. "+
			"List the directories this schema governs, relative to %s.", Display(s.Base))
	}
	var out, missing []string
	for _, r := range s.Roots {
		p := filepath.Clean(filepath.Join(s.Base, r))
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			missing = append(missing, r)
			continue
		}
		out = append(out, p)
	}
	if len(missing) > 0 {
		return nil, errf("scope.yaml lists %s, which do not exist under %s. Fix the path or drop the root.",
			strings.Join(missing, ", "), Display(s.Base))
	}
	return out, nil
}

// Display shows a path relative to the working directory when it sits under it.
func Display(p string) string {
	wd, err := os.Getwd()
	if err != nil {
		return p
	}
	rel, err := filepath.Rel(wd, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return filepath.ToSlash(rel)
}
