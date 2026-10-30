package check

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

func (c *Checker) field(name string, v any, spec *schema.Field, add adder) {
	if !value.Is(v, spec.Value) {
		add("type", name, v, "expected "+spec.Value+", got "+value.TypeName(v))
		return
	}
	m, _ := value.AsMap(v)
	switch spec.Value {
	case "object":
		c.keys(name, m, spec.Schema.Vals, add)
		return
	case "map":
		c.mapping(name, m, spec, add)
		return
	}
	if spec.Value == "list" && len(spec.Items.Vals) > 0 {
		for i, item := range v.([]any) {
			label := name + "[" + strconv.Itoa(i) + "]"
			m, ok := value.AsMap(item)
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
		add("value", label, x, "allowed: "+strings.Join(schema.SortedKeys(enum), " · "))
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
			sub, ok := value.AsMap(m[k])
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

// fieldSet checks one mapping against a doctype's field set: a frontmatter block,
// a whole YAML file, or one entry or item of one. prefix leads every fault's field,
// so a fault inside an entry names the path down to its key. It returns the fields
// required here, `required_when` included.
func (c *Checker) fieldSet(prefix string, fm *value.Mapping, t *schema.Type, skip map[string]bool, add adder) []string {
	required, optional := obligations(fm, t)
	for _, f := range required {
		if _, ok := fm.Get(f); !ok && !skip[f] {
			add("missing", prefix+f, nil, "required on `"+t.Doctype+"`")
		}
	}

	allowed := map[string]bool{}
	for _, f := range required {
		allowed[f] = true
	}
	for _, f := range optional {
		allowed[f] = true
	}
	for _, name := range fm.Keys {
		v := fm.Fields[name]
		label := prefix + name
		if !allowed[name] {
			add("unknown", label, v, "not declared for `"+t.Doctype+"` in types/"+t.Slug+".yaml")
			continue
		}
		spec := t.Resolved[name]
		if spec == nil {
			if len(c.S.Owners[name]) > 0 {
				add("value", label, v, "`"+name+"` does not apply to `"+t.Doctype+"`")
			}
			continue
		}
		c.field(label, v, spec, add)
		if spec.Excludes == "" {
			continue
		}
		other, ok := fm.Get(spec.Excludes)
		if !ok {
			continue
		}
		for _, x := range each(v) {
			for _, y := range each(other) {
				if x != nil && value.Text(x) == value.Text(y) {
					add("value", label, x, "repeats the file's own `"+spec.Excludes+"`")
				}
			}
		}
	}
	return required
}

// obligations are the fields a mapping must and may carry: the doctype's lists, with
// each `required_when` field required where its condition holds and optional elsewhere.
func obligations(fm *value.Mapping, t *schema.Type) (required, optional []string) {
	required = append([]string(nil), t.Required...)
	optional = append([]string(nil), t.Optional...)
	for _, cond := range t.RequiredWhen {
		all := true
		for i, k := range cond.When.Keys {
			got, _ := fm.Get(k)
			if value.Text(got) != value.Text(cond.When.Vals[i]) {
				all = false
				break
			}
		}
		if all {
			required = append(required, cond.Field)
		} else if !slices.Contains(optional, cond.Field) {
			optional = append(optional, cond.Field)
		}
	}
	return required, optional
}
