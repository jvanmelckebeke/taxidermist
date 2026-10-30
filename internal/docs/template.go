package docs

import (
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

func valueType(f *schema.Field) string {
	if f == nil || f.Value == "" {
		return "str"
	}
	return f.Value
}

// nested is the key block of an object field or of a list's items, whichever it has.
func nested(f *schema.Field) []*schema.Field {
	return f.Nested()
}

// template is a field's lines in the "Copy this" block, at the given indent, with
// every nested key, map value and list item below it.
func template(name string, f *schema.Field, tail, in string) []string {
	sub := nested(f)
	switch {
	case f.Value == "map":
		out := []string{in + name + ":" + tail}
		if f.Values != nil {
			return append(out, template("<name>", f.Values, "", in+"  ")...)
		}
		out = append(out, in+"  <name>:")
		for _, k := range sub {
			out = append(out, template(k.Name, k, optMark(k), in+"    ")...)
		}
		return out
	case len(sub) > 0 && f.Value == "object":
		out := []string{in + name + ":" + tail}
		for _, k := range sub {
			out = append(out, template(k.Name, k, optMark(k), in+"  ")...)
		}
		return out
	case len(sub) > 0:
		out := []string{in + name + ":" + tail}
		for i, k := range sub {
			lines := template(k.Name, k, optMark(k), in+"    ")
			if i == 0 {
				lines[0] = in + "  - " + strings.TrimPrefix(lines[0], in+"    ")
			}
			out = append(out, lines...)
		}
		return out
	}
	v := enumKeys(f)
	switch {
	case len(v) > 0 && f.Value == "list":
		return []string{in + name + ": [<" + strings.Join(v, " | ") + ">]" + tail}
	case len(v) > 0:
		return []string{in + name + ": <" + strings.Join(v, " | ") + ">" + tail}
	case f.Value == "list":
		return []string{in + name + ": []" + tail}
	}
	return []string{in + name + ": <" + valueType(f) + ">" + tail}
}

// subKey describes a nested key, or a map's values, as a list item with its allowed
// values below it.
func subKey(name string, k *schema.Field, req string) []string {
	line := "- " + name
	if req != "" {
		line += " · " + req
	}
	line += " · `" + valueType(k) + "`" + matches(k)
	if k.Guidance != "" {
		line += ": " + k.Guidance
	}
	if k.Reference != "" {
		line += " Any " + refTarget(k.Reference) + ", checked."
	}
	out := []string{line}
	for _, d := range k.Enum() {
		line := "  - `" + d.Key + "`: " + d.Means
		if d.Not != "" {
			line += " Not for: " + d.Not
		}
		out = append(out, line)
	}
	return out
}

// enumKeys is a field's allowed values, sorted, once each.
func enumKeys(f *schema.Field) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range f.Enum() {
		if !seen[d.Key] {
			seen[d.Key] = true
			out = append(out, d.Key)
		}
	}
	sort.Strings(out)
	return out
}

func optMark(f *schema.Field) string {
	if f.Required {
		return ""
	}
	return "   # optional"
}
