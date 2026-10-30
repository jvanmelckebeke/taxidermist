package check

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

func (c *Checker) field(name string, v any, spec *schema.Field, add adder) {
	if !TypeOK(v, spec.Value) {
		add("type", name, v, "expected "+spec.Value+", got "+value.TypeName(v))
		return
	}
	if spec.Value == "object" {
		c.keys(name, asMap(v), spec.Schema.Vals, add)
		return
	}
	if spec.Value == "map" {
		c.mapping(name, asMap(v), spec, add)
		return
	}
	if spec.Value == "list" && len(spec.Items.Vals) > 0 {
		for i, item := range v.([]any) {
			label := name + "[" + strconv.Itoa(i) + "]"
			m, ok := asMapOK(item)
			if !ok {
				add("shape", label, item, "expected a mapping with "+strings.Join(sortedNames(spec.Items.Vals), " · ")+", got "+value.TypeName(item))
				continue
			}
			c.keys(label, m, spec.Items.Vals, add)
		}
		return
	}
	_, isList := v.([]any)
	for i, x := range each(v) {
		label := name
		if isList {
			label = name + "[" + strconv.Itoa(i) + "]"
		}
		c.element(label, x, spec, add)
	}
}

// element checks one value, or one element of a list, against the field's reference
// or its allowed values.
func (c *Checker) element(label string, x any, spec *schema.Field, add adder) {
	if x == nil {
		return
	}
	if value.IsScalar(x) && !spec.Matches(value.Text(x)) {
		add("value", label, x, "does not match `"+spec.Pattern+"`")
	}
	if spec.Reference != "" {
		c.references(label, x, spec.Reference, add)
		return
	}
	if enum := spec.Enum(); len(enum) > 0 && !member(x, enum) {
		add("value", label, x, "allowed: "+strings.Join(sortedKeys(enum), " · "))
	}
}

// keys checks one mapping against a `schema:` or `items:` block: each declared key
// gets the same checks as a top-level field, and nothing else is allowed.
func (c *Checker) keys(label string, m map[string]any, sub []*schema.Field, add adder) {
	known := map[string]bool{}
	for _, rule := range sub {
		known[rule.Name] = true
		got, ok := m[rule.Name]
		key := label + "." + rule.Name
		if !ok {
			if rule.Required {
				add("missing", key, nil, "required key of `"+label+"`")
			}
			continue
		}
		c.field(key, got, rule, add)
	}
	extra := make([]string, 0)
	for k := range m {
		if !known[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		add("shape", label+"."+k, m[k], "not a key of `"+label+"`. Allowed: "+strings.Join(sortedNames(sub), " · "))
	}
}

// mapping checks a `value: map`: every key against `key:`, and every value against
// `schema:`, as an object's keys, or against `values:`, as a field.
func (c *Checker) mapping(label string, m map[string]any, spec *schema.Field, add adder) {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		key := label + "." + k
		if spec.Key != nil {
			c.element(key, k, spec.Key, add)
		}
		switch {
		case len(spec.Schema.Vals) > 0:
			sub, ok := asMapOK(m[k])
			if !ok {
				add("shape", key, m[k], "expected a mapping with "+strings.Join(sortedNames(spec.Schema.Vals), " · ")+", got "+value.TypeName(m[k]))
				continue
			}
			c.keys(key, sub, spec.Schema.Vals, add)
		case spec.Values != nil:
			c.field(key, m[k], spec.Values, add)
		}
	}
}

func asMapOK(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, x := range m {
			out[value.Text(k)] = x
		}
		return out, true
	}
	return nil, false
}

func asMap(v any) map[string]any {
	m, _ := asMapOK(v)
	return m
}

// TypeOK checks a value against a `value:` spec, possibly an `a|b` union. An empty
// spec checks nothing.
func TypeOK(v any, spec string) bool {
	if spec == "" {
		return true
	}
	for _, alt := range strings.Split(spec, "|") {
		switch strings.TrimSpace(alt) {
		case "str":
			if _, ok := v.(string); ok {
				return true
			}
		case "list":
			if _, ok := v.([]any); ok {
				return true
			}
		case "bool":
			if _, ok := v.(bool); ok {
				return true
			}
		case "object", "map":
			if _, ok := asMapOK(v); ok {
				return true
			}
		case "date":
			if _, ok := v.(time.Time); ok {
				return true
			}
		case "int":
			switch v.(type) {
			case int, int64, uint64:
				return true
			}
		case "float":
			switch v.(type) {
			case int, int64, uint64, float64:
				return true
			}
		}
	}
	return false
}
