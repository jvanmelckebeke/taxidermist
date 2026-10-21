package schema

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

// Def is one allowed value and what it means.
type Def struct {
	Key   string
	Means string
	Not   string // what the value is not for, where a neighbour is easy to confuse with it
	Group string // the vocabulary group it sits under, if any
}

// Meaning is a definition: a one-line string, or a mapping with `means:` and an
// optional `not:` that says what the value is not for.
type Meaning struct {
	Means string `yaml:"means"`
	Not   string `yaml:"not"`
}

func (m *Meaning) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		return n.Decode(&m.Means)
	}
	if err := strict(n, "a definition", "means", "not"); err != nil {
		return err
	}
	type plain Meaning
	if err := n.Decode((*plain)(m)); err != nil {
		return err
	}
	if m.Means == "" {
		return fmt.Errorf("line %d: a definition mapping needs `means:`", n.Line)
	}
	return nil
}

// isMeaning tells a `{means, not}` definition from a vocabulary group, which is also
// a mapping: a definition is the mapping with a `means` or `not` key, so neither can
// be a value name inside a group.
func isMeaning(n *yaml.Node) bool {
	if n.Kind != yaml.MappingNode {
		return true
	}
	for i := 0; i < len(n.Content); i += 2 {
		if k := n.Content[i].Value; k == "means" || k == "not" {
			return true
		}
	}
	return false
}

// Field is a field spec, or a key of an object field's `schema:` or a list's `items:`.
type Field struct {
	Name        string           `yaml:"-"`
	Value       string           `yaml:"value"` // a type name or an `a|b` union; empty checks nothing
	Guidance    string           `yaml:"guidance"`
	Definitions Ordered[Meaning] `yaml:"definitions"`
	Vocabulary  string           `yaml:"vocabulary"`
	Dynamic     bool             `yaml:"dynamic"`
	Schema      Ordered[*Field]  `yaml:"schema"` // keys of a `value: object`
	Items       Ordered[*Field]  `yaml:"items"`  // keys of each mapping in a `value: list`
	Reference   string           `yaml:"reference"`
	Required    bool             `yaml:"required"`   // nested keys only
	AppliesTo   []string         `yaml:"applies_to"` // singletons only
	Excludes    string           `yaml:"excludes"`   // a field whose value this one never repeats
	defs        []Def
}

func (f *Field) UnmarshalYAML(n *yaml.Node) error {
	if err := strict(n, "a field", "value", "guidance", "definitions", "vocabulary", "dynamic",
		"schema", "items", "reference", "required", "applies_to", "excludes"); err != nil {
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
		f.defs = append(f.defs, Def{Key: k, Means: f.Definitions.Vals[i].Means, Not: f.Definitions.Vals[i].Not})
	}
	for _, sub := range append(append([]*Field(nil), f.Schema.Vals...), f.Items.Vals...) {
		if err := s.bind(sub, where+": "+f.Name); err != nil {
			return err
		}
	}
	return nil
}
