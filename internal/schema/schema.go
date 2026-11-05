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
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

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
	Vocabularies map[string]*Vocabulary
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
	segments, err := s.loadScope()
	if err != nil {
		return nil, err
	}
	// Each step may use what the steps before it loaded: segments and fields bind to
	// vocabularies, types compose traits and singletons, and a vocabulary's source
	// must be governed by a type.
	steps := []func() error{
		s.loadExempt,
		s.loadVocabularies,
		func() error { return s.loadSegments(segments) },
		s.loadTraits,
		s.loadSingletons,
		s.loadTypes,
		s.checkReferenceTypes,
		s.checkSources,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// checkReferenceTypes rejects a `reference_type:` that names no markdown doctype,
// which no page could ever satisfy.
func (s *Schema) checkReferenceTypes() error {
	var walk func(f *Field, where string) error
	walk = func(f *Field, where string) error {
		if f == nil {
			return nil
		}
		if f.RefType != "" {
			if t := s.Types[f.RefType]; t == nil || t.IsYAML() {
				return errf("%s: field `%s` has `reference_type: %s`, which is no page doctype. Known: %s.",
					where, f.Name, f.RefType, strings.Join(s.PageDoctypes(), ", "))
			}
		}
		for _, sub := range append(append([]*Field{f.Key, f.Values}, f.Schema.Vals...), f.Items.Vals...) {
			if err := walk(sub, where+": "+f.Name); err != nil {
				return err
			}
		}
		return nil
	}
	for _, f := range s.Singletons {
		if err := walk(f, "singletons.yaml"); err != nil {
			return err
		}
	}
	for _, tr := range s.Traits {
		for _, f := range tr.Fields {
			if err := walk(f, "traits.yaml: "+tr.Name); err != nil {
				return err
			}
		}
	}
	for _, dt := range s.Doctypes() {
		t := s.Types[dt]
		for _, f := range append([]*Field{t.Key}, t.Fields.Vals...) {
			if err := walk(f, "types/"+t.Slug+".yaml"); err != nil {
				return err
			}
		}
	}
	return nil
}

// VocabularyNames returns the vocabulary names, sorted.
func (s *Schema) VocabularyNames() []string {
	out := make([]string, 0, len(s.Vocabularies))
	for v := range s.Vocabularies {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// HasYAML is true when some doctype governs YAML files. Until one does, a YAML file
// under a governed root is not taxidermist's business.
func (s *Schema) HasYAML() bool {
	for _, t := range s.Types {
		if t.IsYAML() {
			return true
		}
	}
	return false
}

// TypesOf returns the doctypes whose `files:` glob matches rel, a slash path relative
// to Base, sorted by doctype.
func (s *Schema) TypesOf(rel string) []*Type {
	var out []*Type
	for _, dt := range s.Doctypes() {
		if t := s.Types[dt]; t.IsYAML() && Fnmatch(rel, t.Files) {
			out = append(out, t)
		}
	}
	return out
}

// PageDoctypes are the doctypes a markdown page's `type:` may name, sorted.
func (s *Schema) PageDoctypes() []string {
	var out []string
	for _, dt := range s.Doctypes() {
		if !s.Types[dt].IsYAML() {
			out = append(out, dt)
		}
	}
	return out
}

// Globs lists every YAML doctype's `files:`, in doctype order.
func (s *Schema) Globs() []string {
	var out []string
	for _, dt := range s.Doctypes() {
		if t := s.Types[dt]; t.IsYAML() {
			out = append(out, t.Files)
		}
	}
	return out
}

// IsYAMLPath is true for a .yaml or .yml path.
func IsYAMLPath(p string) bool {
	return strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")
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
