package check

import (
	"regexp"
	"strings"
)

// Fnmatch is Python's fnmatch.fnmatch on POSIX: `*` crosses `/`, so `a/**` and
// `a/*` both match everything under a/.
func Fnmatch(name, pattern string) bool {
	re, err := regexp.Compile(translate(pattern))
	return err == nil && re.MatchString(name)
}

func translate(pat string) string {
	var b strings.Builder
	b.WriteString(`(?s:`)
	for i := 0; i < len(pat); i++ {
		c := pat[i]
		switch c {
		case '*':
			b.WriteString(`.*`)
		case '?':
			b.WriteString(`.`)
		case '[':
			j := i + 1
			if j < len(pat) && pat[j] == '!' {
				j++
			}
			if j < len(pat) && pat[j] == ']' {
				j++
			}
			for j < len(pat) && pat[j] != ']' {
				j++
			}
			if j >= len(pat) {
				b.WriteString(`\[`)
				continue
			}
			class := pat[i+1 : j]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			} else if strings.HasPrefix(class, "^") {
				class = `\` + class
			}
			class = strings.ReplaceAll(class, `\`, `\\`)
			class = strings.Replace(class, `\\^`, `\^`, 1)
			b.WriteString("[" + class + "]")
			i = j
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`)\z`)
	return "^" + b.String()
}
