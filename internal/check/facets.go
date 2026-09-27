package check

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
	"github.com/jvanmelckebeke/taxidermist/internal/pyyaml"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

// Facets prints every key and value in use per doctype, doctypes with no schema
// first. It is the drift report: what says `index` and `Index` are the same idea
// spelled twice, before anyone decides which one wins. A `!` marks a key the doctype
// does not declare.
func Facets(w io.Writer, s *schema.Schema, files []string) {
	type counter struct {
		order  []string
		counts map[string]int
	}
	seen := map[string]map[string]*counter{}
	keyOrder := map[string][]string{}
	for _, p := range files {
		fm, err := frontmatter.Read(p)
		if err != nil || fm.Kind != pyyaml.Map || len(fm.Keys) == 0 {
			continue
		}
		dtv, _ := fm.Get("type")
		dt := dtv.String()
		if seen[dt] == nil {
			seen[dt] = map[string]*counter{}
		}
		for i, k := range fm.Keys {
			v := fm.Vals[i]
			cell := pyyaml.Truncate(v.String(), 40)
			switch v.Kind {
			case pyyaml.List:
				cell = "<list>"
			case pyyaml.Map:
				cell = "<object>"
			}
			key := k.String()
			c := seen[dt][key]
			if c == nil {
				c = &counter{counts: map[string]int{}}
				seen[dt][key] = c
				keyOrder[dt] = append(keyOrder[dt], key)
			}
			if c.counts[cell] == 0 {
				c.order = append(c.order, cell)
			}
			c.counts[cell]++
		}
	}
	doctypes := make([]string, 0, len(seen))
	for dt := range seen {
		doctypes = append(doctypes, dt)
	}
	sort.Slice(doctypes, func(i, j int) bool {
		gi, gj := s.Types[doctypes[i]] != nil, s.Types[doctypes[j]] != nil
		if gi != gj {
			return !gi
		}
		return doctypes[i] < doctypes[j]
	})
	total := func(c *counter) int {
		n := 0
		for _, x := range c.counts {
			n += x
		}
		return n
	}
	for _, dt := range doctypes {
		n := 0
		if c := seen[dt]["type"]; c != nil {
			n = total(c)
		}
		mark := ""
		t := s.Types[dt]
		if t == nil {
			mark = "   [no schema]"
		}
		fmt.Fprintf(w, "\n%s  (%d files)%s\n", dt, n, mark)
		var known map[string]bool
		if t != nil {
			known = t.Declared()
		}
		keys := append([]string(nil), keyOrder[dt]...)
		sort.SliceStable(keys, func(i, j int) bool { return total(seen[dt][keys[i]]) > total(seen[dt][keys[j]]) })
		for _, k := range keys {
			c := seen[dt][k]
			flag := " "
			if len(known) > 0 && !known[k] {
				flag = "!"
			}
			vals := append([]string(nil), c.order...)
			sort.SliceStable(vals, func(i, j int) bool { return c.counts[vals[i]] > c.counts[vals[j]] })
			var top []string
			for i, v := range vals {
				if i == 6 {
					break
				}
				top = append(top, fmt.Sprintf("%s(%d)", v, c.counts[v]))
			}
			more := ""
			if len(vals) > 6 {
				more = fmt.Sprintf(" ...+%d", len(vals)-6)
			}
			fmt.Fprintf(w, "  %s %s: %d/%d  %s%s\n", flag, k, total(c), n, strings.Join(top, ", "), more)
		}
	}
}
