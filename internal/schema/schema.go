// Package schema loads a taxonomy directory.
//
//	scope.yaml           the roots the schema governs, how it treats doctypes it has no file for,
//	                     and the folders whose names must be vocabulary values
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
	"reflect"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Trait struct {
	Name   string
	Fields []*Field
}

type Schema struct {
	Dir          string // the taxonomy directory
	Base         string // its parent: roots, references and exemptions resolve against it
	Roots        []string
	Ungoverned   string // "error" or "report"
	Invocation   string // how this repo runs taxidermist, with `{command}` where the subcommand goes
	Segments     []*Segment
	Types        map[string]*Type
	Traits       []*Trait
	Singletons   []*Field
	Vocabularies map[string][]Def
	Exempt       []Exemption
	Owners       map[string]map[string]bool // field -> doctypes that declare it
}

// Command is how a reader of the generated pages runs one taxidermist subcommand in
// this repo, from scope.yaml's `invocation:`.
func (s *Schema) Command(sub string) string {
	return strings.Replace(s.Invocation, "{command}", sub, 1)
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
		Roots      []string     `yaml:"roots"`
		Ungoverned string       `yaml:"ungoverned"`
		Invocation string       `yaml:"invocation"`
		Segments   []rawSegment `yaml:"segments"`
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
	s.Invocation = strings.TrimSpace(scope.Invocation)
	if s.Invocation == "" {
		s.Invocation = "taxidermist"
	}
	if !strings.Contains(s.Invocation, "{command}") {
		s.Invocation += " {command}"
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
	for i, r := range scope.Segments {
		seg, err := s.segment(r)
		if err != nil {
			return nil, errf("scope.yaml segment %d: %v", i+1, err)
		}
		s.Segments = append(s.Segments, seg)
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

// read decodes one schema file. A missing file is empty. A top-level key the struct
// has no `yaml:` tag for is an error, unless the type checks its own keys.
func read(dir, rel string, out any) error {
	raw, err := os.ReadFile(filepath.Join(dir, rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return errf("%s: %v", rel, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Tag == "!!null" {
		return nil
	}
	if _, own := out.(yaml.Unmarshaler); !own {
		if err := strict(doc.Content[0], rel, keys(out)...); err != nil {
			return errf("%s", err)
		}
	}
	if err := doc.Content[0].Decode(out); err != nil {
		return errf("%s: %v", rel, err)
	}
	return nil
}

// keys returns the `yaml:` names of the fields of the struct out points to.
func keys(out any) []string {
	t := reflect.TypeOf(out).Elem()
	var ks []string
	for i := 0; i < t.NumField(); i++ {
		if k, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ","); k != "" && k != "-" {
			ks = append(ks, k)
		}
	}
	return ks
}
