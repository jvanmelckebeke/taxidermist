// Package value names and prints the Go values yaml.v3 decodes frontmatter into.
package value

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TypeName is the schema's name for a value's type.
func TypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "str"
	case bool:
		return "bool"
	case int, int64, uint64:
		return "int"
	case float64:
		return "float"
	case time.Time:
		return "date"
	case []any:
		return "list"
	case map[string]any, map[any]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

// IsScalar is true for anything that is not a list or a mapping.
func IsScalar(v any) bool {
	switch v.(type) {
	case []any, map[string]any, map[any]any:
		return false
	}
	return true
}

// Text prints a value the way it would be written in YAML. A date at midnight UTC
// prints as a bare date, so it compares equal to the date a schema lists.
func Text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case time.Time:
		if x.Equal(time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.UTC)) {
			return x.Format("2006-01-02")
		}
		return x.Format(time.RFC3339Nano)
	case []any:
		parts := make([]string, len(x))
		for i, it := range x {
			parts[i] = Text(it)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ": " + Text(x[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[Text(k)] = val
		}
		return Text(m)
	}
	return fmt.Sprint(v)
}

// Truncate cuts a string to n characters.
func Truncate(s string, n int) string {
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}
