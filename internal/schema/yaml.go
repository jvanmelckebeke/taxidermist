package schema

import (
	"fmt"
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
