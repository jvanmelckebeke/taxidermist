package schema

import (
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

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
