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
	if spec.Reference != "" {
		c.references(name, v, spec.Reference, add)
		return
	}
	enum := spec.Enum()
	if len(enum) == 0 {
		return
	}
	for _, x := range each(v) {
		if x != nil && !member(x, enum) {
			add("value", name, x, "allowed: "+strings.Join(sortedKeys(enum), " · "))
		}
	}
}

// keys checks one mapping against a `schema:` or `items:` block: declared keys typed,
// nothing else.
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
		if !TypeOK(got, rule.Value) {
			add("type", key, got, "expected "+rule.Value+", got "+value.TypeName(got))
			continue
		}
		if enum := rule.Enum(); len(enum) > 0 && !member(got, enum) {
			var keys []string
			for _, d := range enum {
				keys = append(keys, d.Key)
			}
			add("value", key, got, "not a defined value. Allowed: "+strings.Join(keys, " · "))
			continue
		}
		if rule.Reference != "" {
			c.references(key, got, rule.Reference, add)
		}
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
		case "object":
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
