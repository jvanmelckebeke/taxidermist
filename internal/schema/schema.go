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

	"go.yaml.in/yaml/v3"
)

// Ordered is a YAML mapping that keeps the order its keys are written in.
type Ordered[T any] struct {
	Keys []string
	Vals []T
}

func (o *Ordered[T]) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a mapping", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		var v T
		if err := n.Content[i+1].Decode(&v); err != nil {
			return err
		}
		o.Keys = append(o.Keys, n.Content[i].Value)
		o.Vals = append(o.Vals, v)
	}
	return nil
}

// strict rejects mapping keys outside the allowed set, so a typo in the schema
// (`defintions:`) is an error rather than a field that silently checks nothing.
func strict(n *yaml.Node, what string, allowed ...string) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: %s is not a mapping", n.Line, what)
	}
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		ok := false
		for _, a := range allowed {
			ok = ok || k.Value == a
		}
		if !ok {
			return fmt.Errorf("line %d: %s has unknown key `%s`; it takes %s", k.Line, what, k.Value, strings.Join(allowed, ", "))
		}
	}
	return nil
}

// Def is one allowed value and what it means.
type Def struct {
	Key   string
	Means string
	Group string // the vocabulary group it sits under, if any
}

// Field is a field spec, or a key of an object field's `schema:` or a list's `items:`.
type Field struct {
	Name        string          `yaml:"-"`
	Value       string          `yaml:"value"` // a type name or an `a|b` union; empty checks nothing
	Guidance    string          `yaml:"guidance"`
	Definitions Ordered[string] `yaml:"definitions"`
	Vocabulary  string          `yaml:"vocabulary"`
	Dynamic     bool            `yaml:"dynamic"`
	Schema      Ordered[*Field] `yaml:"schema"` // keys of a `value: object`
	Items       Ordered[*Field] `yaml:"items"`  // keys of each mapping in a `value: list`
	Reference   string          `yaml:"reference"`
	Required    bool            `yaml:"required"`   // nested keys only
	AppliesTo   []string        `yaml:"applies_to"` // singletons only
	defs        []Def
}

func (f *Field) UnmarshalYAML(n *yaml.Node) error {
	if err := strict(n, "a field", "value", "guidance", "definitions", "vocabulary", "dynamic",
		"schema", "items", "reference", "required", "applies_to"); err != nil {
		return err
	}
	type plain Field
	if err := n.Decode((*plain)(f)); err != nil {
		return err
	}
	for i, name := range f.Schema.Keys {
		f.Schema.Vals[i].Name = name
	}
	for i, name := range f.Items.Keys {
		f.Items.Vals[i].Name = name
	}
	return nil
}

// Nested is the key block of an object field or of a list's items.
func (f *Field) Nested() []*Field {
	if f.Value == "object" {
		return f.Schema.Vals
	}
	return f.Items.Vals
}

// Enum is the field's allowed values: its definitions, or its vocabulary's. Nil when
// the field is free, dynamic, or a reference.
func (f *Field) Enum() []Def {
	if f.Dynamic {
		return nil
	}
	return f.defs
}

// Condition makes a field required when every listed field has the listed value.
type Condition struct {
	Field string
	When  Ordered[any]
}

// Type is one doctype.
type Type struct {
	Doctype      string            `yaml:"type"` // the literal `type:` value; defaults to the slug
	Slug         string            `yaml:"-"`    // the file stem, used for the format page name
	Traits       []string          `yaml:"traits"`
	Required     []string          `yaml:"required"`
	Optional     []string          `yaml:"optional"`
	RequiredWhen []Condition       `yaml:"-"`
	Fields       Ordered[*Field]   `yaml:"fields"`
	Resolved     map[string]*Field `yaml:"-"` // own fields, then singletons, then traits
}

func (t *Type) UnmarshalYAML(n *yaml.Node) error {
	if err := strict(n, "a type file", "type", "traits", "required", "optional", "required_when", "fields"); err != nil {
		return err
	}
	type plain Type
	var raw struct {
		plain        `yaml:",inline"`
		RequiredWhen Ordered[Ordered[any]] `yaml:"required_when"`
	}
	if err := n.Decode(&raw); err != nil {
		return err
	}
	*t = Type(raw.plain)
	for i, f := range raw.RequiredWhen.Keys {
		t.RequiredWhen = append(t.RequiredWhen, Condition{Field: f, When: raw.RequiredWhen.Vals[i]})
	}
	return nil
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

type Exemption struct {
	Paths string   `yaml:"paths"`
	Omit  []string `yaml:"omit"`
	Why   string   `yaml:"why"`
}

type Schema struct {
	Dir          string // the taxonomy directory
	Base         string // its parent: roots, references and exemptions resolve against it
	Roots        []string
	Ungoverned   string // "error" or "report"
	Types        map[string]*Type
	Traits       []*Trait
	Singletons   []*Field
	Vocabularies map[string][]Def
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
		Types: map[string]*Type{}, Vocabularies: map[string][]Def{},
		Owners: map[string]map[string]bool{},
	}

	var scope struct {
		Roots      []string `yaml:"roots"`
		Ungoverned string   `yaml:"ungoverned"`
	}
	if err := read(abs, "scope.yaml", &scope); err != nil {
		return nil, err
	}
	s.Roots, s.Ungoverned = scope.Roots, scope.Ungoverned
	if s.Ungoverned == "" {
		s.Ungoverned = "error"
	}
	if s.Ungoverned != "error" && s.Ungoverned != "report" {
		return nil, errf("scope.yaml sets `ungoverned: %s`; it takes `error` or `report`.", s.Ungoverned)
	}

	var exempt struct {
		Exempt []Exemption `yaml:"exempt"`
	}
	if err := read(abs, "exempt.yaml", &exempt); err != nil {
		return nil, err
	}
	for i, e := range exempt.Exempt {
		if e.Paths == "" {
			return nil, errf("exempt.yaml entry %d has no `paths:` glob.", i+1)
		}
	}
	s.Exempt = exempt.Exempt

	if err := s.loadVocabularies(); err != nil {
		return nil, err
	}

	var traits struct {
		Traits Ordered[Ordered[*Field]] `yaml:"traits"`
	}
	if err := read(abs, "traits.yaml", &traits); err != nil {
		return nil, err
	}
	for i, name := range traits.Traits.Keys {
		t := &Trait{Name: name}
		fs := traits.Traits.Vals[i]
		for j, fname := range fs.Keys {
			f := fs.Vals[j]
			f.Name = fname
			if err := s.bind(f, "traits.yaml"); err != nil {
				return nil, err
			}
			t.Fields = append(t.Fields, f)
		}
		s.Traits = append(s.Traits, t)
	}

	var singles struct {
		Fields Ordered[*Field] `yaml:"fields"`
	}
	if err := read(abs, "singletons.yaml", &singles); err != nil {
		return nil, err
	}
	for i, name := range singles.Fields.Keys {
		f := singles.Fields.Vals[i]
		f.Name = name
		if err := s.bind(f, "singletons.yaml"); err != nil {
			return nil, err
		}
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

// read decodes one schema file. A missing file is empty.
func read(dir, rel string, out any) error {
	raw, err := os.ReadFile(filepath.Join(dir, rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(raw, out); err != nil {
		return errf("%s: %v", rel, err)
	}
	return nil
}

func (s *Schema) loadVocabularies() error {
	files, _ := filepath.Glob(filepath.Join(s.Dir, "vocabularies", "*.yaml"))
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".yaml")
		rel := "vocabularies/" + name + ".yaml"
		var v struct {
			Definitions Ordered[yaml.Node] `yaml:"definitions"`
		}
		if err := read(s.Dir, rel, &v); err != nil {
			return err
		}
		if len(v.Definitions.Keys) == 0 {
			return errf("%s has no `definitions:` mapping.", rel)
		}
		var defs []Def
		for i, key := range v.Definitions.Keys {
			node := v.Definitions.Vals[i]
			if node.Kind == yaml.MappingNode {
				var group Ordered[string]
				if err := node.Decode(&group); err != nil {
					return errf("%s: group `%s`: %v", rel, key, err)
				}
				for j, k := range group.Keys {
					defs = append(defs, Def{Key: k, Means: group.Vals[j], Group: key})
				}
				continue
			}
			var means string
			if err := node.Decode(&means); err != nil {
				return errf("%s: `%s`: %v", rel, key, err)
			}
			defs = append(defs, Def{Key: key, Means: means})
		}
		s.Vocabularies[name] = defs
	}
	return nil
}

// bind resolves a field's allowed values, for it and every nested key.
func (s *Schema) bind(f *Field, where string) error {
	if f.Vocabulary != "" {
		if len(f.Definitions.Keys) > 0 {
			return errf("%s: field `%s` has both `definitions:` and `vocabulary:`. Keep one list.", where, f.Name)
		}
		defs, ok := s.Vocabularies[f.Vocabulary]
		if !ok {
			return errf("%s: field `%s` names vocabulary `%s`, and vocabularies/%s.yaml does not exist.",
				where, f.Name, f.Vocabulary, f.Vocabulary)
		}
		f.defs = defs
	}
	for i, k := range f.Definitions.Keys {
		f.defs = append(f.defs, Def{Key: k, Means: f.Definitions.Vals[i]})
	}
	for _, sub := range append(append([]*Field(nil), f.Schema.Vals...), f.Items.Vals...) {
		if err := s.bind(sub, where+": "+f.Name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Schema) loadType(file string) error {
	slug := strings.TrimSuffix(filepath.Base(file), ".yaml")
	rel := "types/" + filepath.Base(file)
	t := &Type{}
	if err := read(s.Dir, rel, t); err != nil {
		return err
	}
	t.Slug = slug
	if t.Doctype == "" {
		t.Doctype = slug
	}
	t.Resolved = map[string]*Field{}
	if other, dup := s.Types[t.Doctype]; dup {
		return errf("types/%s.yaml and %s both declare `type: %s`.", other.Slug, rel, t.Doctype)
	}
	for i, name := range t.Fields.Keys {
		f := t.Fields.Vals[i]
		f.Name = name
		if err := s.bind(f, rel); err != nil {
			return err
		}
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
	for _, f := range t.Fields.Vals {
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
