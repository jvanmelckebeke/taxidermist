package value

import (
	"sort"

	"go.yaml.in/yaml/v3"
)

// Mapping is a decoded YAML mapping that keeps its key order: a frontmatter block, a
// whole YAML file, or one entry or item of one. Keys holds the keys in the order the
// file writes them.
type Mapping struct {
	Keys   []string
	Fields map[string]any
}

// Get returns a key's value and whether the mapping has the key.
func (m *Mapping) Get(key string) (any, bool) {
	v, ok := m.Fields[key]
	return v, ok
}

// MappingOf decodes a mapping node, keeping its key order.
func MappingOf(m *yaml.Node) (*Mapping, error) {
	d := &Mapping{}
	if err := m.Decode(&d.Fields); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i].Value
		if _, ok := d.Fields[k]; ok && !seen[k] {
			seen[k] = true
			d.Keys = append(d.Keys, k)
		}
	}
	// Keys that arrived through a `<<` merge, after the ones written out.
	var merged []string
	for k := range d.Fields {
		if !seen[k] {
			merged = append(merged, k)
		}
	}
	sort.Strings(merged)
	d.Keys = append(d.Keys, merged...)
	return d, nil
}
