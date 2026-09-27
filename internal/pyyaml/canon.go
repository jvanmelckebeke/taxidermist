package pyyaml

import (
	"encoding/json"
	"strings"
)

// Canon renders a value in the canonical form scripts/pyyaml-parity/canon.py prints
// for PyYAML's result, so the two parsers can be diffed line by line.
func Canon(v Value) string {
	switch v.Kind {
	case Null:
		return "null"
	case Bool:
		return "bool:" + v.String()
	case Int:
		return "int:" + v.String()
	case Float:
		return "float:" + v.String()
	case Str:
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(v.S)
		return "str:" + pyJSON(strings.TrimSuffix(b.String(), "\n"))
	case Date:
		return "date:" + v.String()
	case Datetime:
		return "datetime:" + v.String()
	case List:
		parts := make([]string, len(v.Items))
		for i, it := range v.Items {
			parts[i] = Canon(it)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case Map:
		parts := make([]string, len(v.Keys))
		for i := range v.Keys {
			parts[i] = Canon(v.Keys[i]) + "=" + Canon(v.Vals[i])
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return "other"
}

// pyJSON rewrites Go's JSON escapes into the ones Python's json.dumps emits.
func pyJSON(s string) string {
	r := strings.NewReplacer(`&`, "&", `<`, "<", `>`, ">", ` `, " ", ` `, " ")
	s = r.Replace(s)
	var b strings.Builder
	for _, c := range s {
		if c == 0x7f {
			b.WriteString(`\u007f`)
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}
