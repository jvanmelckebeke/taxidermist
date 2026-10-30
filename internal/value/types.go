package value

import (
	"strings"
	"time"
)

// AsMap returns a decoded mapping with string keys, as written in YAML.
func AsMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, x := range m {
			out[Text(k)] = x
		}
		return out, true
	}
	return nil, false
}

// Is checks a value against a `value:` spec, possibly an `a|b` union. An empty
// spec checks nothing.
func Is(v any, spec string) bool {
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
			if _, ok := AsMap(v); ok {
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
