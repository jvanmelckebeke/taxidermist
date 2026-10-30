package schema

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Source is where a vocabulary read with `from:` takes its values: the keys of the
// mapping at At in the data file From, each meaning its entry's Means key.
type Source struct {
	From  string // relative to Base
	At    string // a dotted key path; empty for the file's top level
	Means string // the key in each entry that holds its meaning; empty for none
}

func (s *Schema) loadVocabularies() error {
	files, _ := filepath.Glob(filepath.Join(s.Dir, "vocabularies", "*.yaml"))
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".yaml")
		rel := "vocabularies/" + name + ".yaml"
		var v struct {
			Definitions Ordered[yaml.Node] `yaml:"definitions"`
			Source      `yaml:",inline"`
		}
		if err := read(s.Dir, rel, &v); err != nil {
			return err
		}
		if v.From != "" {
			if len(v.Definitions.Keys) > 0 {
				return errf("%s has both `from:` and `definitions:`. Keep one list.", rel)
			}
			defs, err := s.readSource(rel, v.Source)
			if err != nil {
				return err
			}
			s.Vocabularies[name], s.Sources[name] = defs, v.Source
			continue
		}
		if v.At != "" || v.Means != "" {
			return errf("%s: `at:` and `means:` go with `from:`.", rel)
		}
		if len(v.Definitions.Keys) == 0 {
			return errf("%s has no `definitions:` mapping.", rel)
		}
		var defs []Def
		for i, key := range v.Definitions.Keys {
			node := v.Definitions.Vals[i]
			if !isMeaning(&node) {
				var group Ordered[Meaning]
				if err := node.Decode(&group); err != nil {
					return errf("%s: group `%s`: %v", rel, key, err)
				}
				for j, k := range group.Keys {
					defs = append(defs, Def{Key: k, Means: group.Vals[j].Means, Not: group.Vals[j].Not, Group: key})
				}
				continue
			}
			var m Meaning
			if err := node.Decode(&m); err != nil {
				return errf("%s: `%s`: %v", rel, key, err)
			}
			defs = append(defs, Def{Key: key, Means: m.Means, Not: m.Not})
		}
		s.Vocabularies[name] = defs
	}
	return nil
}

// readSource reads a `from:` vocabulary's values out of its data file.
func (s *Schema) readSource(rel string, src Source) ([]Def, error) {
	if filepath.IsAbs(src.From) || slices.Contains(strings.Split(filepath.ToSlash(src.From), "/"), "..") {
		return nil, errf("%s: `from: %s` must be a file in this repo, relative to %s.", rel, src.From, Display(s.Base))
	}
	raw, err := os.ReadFile(filepath.Join(s.Base, src.From))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errf("%s reads from %s, which does not exist.", rel, src.From)
	}
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, errf("%s reads from %s, which does not parse: %v", rel, src.From, err)
	}
	var m *yaml.Node
	if len(root.Content) > 0 {
		m = root.Content[0]
	}
	where := src.From
	if src.At != "" {
		for _, k := range strings.Split(src.At, ".") {
			m = Lookup(m, k)
			where += " at `" + k + "`"
		}
	}
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, errf("%s reads from %s, which holds no mapping there.", rel, where)
	}
	var defs []Def
	for i := 0; i+1 < len(m.Content); i += 2 {
		d := Def{Key: m.Content[i].Value}
		if src.Means != "" {
			if v := Lookup(m.Content[i+1], src.Means); v != nil && v.Kind == yaml.ScalarNode {
				d.Means = v.Value
			}
		}
		defs = append(defs, d)
	}
	return defs, nil
}

// Lookup returns the value of key in a mapping node, or nil.
func Lookup(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
