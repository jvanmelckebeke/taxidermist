package check

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

// Facets prints every key and value in use per doctype, doctypes with no schema
// first. It is the drift report: what says `index` and `Index` are the same idea
// spelled twice, before anyone decides which one wins. A `!` marks a key the doctype
// does not declare. Each segment rule then lists the values that have no folder yet,
// which is fine: a value can be defined before anything is filed under it.
func Facets(w io.Writer, s *schema.Schema, files []string) {
	f := tallyFacets(files)
	for _, dt := range f.doctypes(s) {
		f.print(w, s, dt)
	}
	segmentFacets(w, s, files)
}

// counter tallies one key's values, in the order each first appears.
type counter struct {
	order  []string
	counts map[string]int
}

func (c *counter) total() int {
	n := 0
	for _, x := range c.counts {
		n += x
	}
	return n
}

// facets is every key in use per doctype, each with its values, read from the
// files' frontmatter.
type facets struct {
	seen     map[string]map[string]*counter // doctype -> key -> its values
	keyOrder map[string][]string            // doctype -> keys, in the order each first appears
}

func tallyFacets(files []string) *facets {
	f := &facets{seen: map[string]map[string]*counter{}, keyOrder: map[string][]string{}}
	for _, p := range files {
		fm, err := frontmatter.Read(p)
		if err != nil || fm == nil || len(fm.Keys) == 0 {
			continue
		}
		dtv, _ := fm.Get("type")
		dt := value.Text(dtv)
		if f.seen[dt] == nil {
			f.seen[dt] = map[string]*counter{}
		}
		for _, key := range fm.Keys {
			v := fm.Fields[key]
			cell := value.Truncate(value.Text(v), 40)
			switch value.TypeName(v) {
			case "list":
				cell = "<list>"
			case "object":
				cell = "<object>"
			}
			c := f.seen[dt][key]
			if c == nil {
				c = &counter{counts: map[string]int{}}
				f.seen[dt][key] = c
				f.keyOrder[dt] = append(f.keyOrder[dt], key)
			}
			if c.counts[cell] == 0 {
				c.order = append(c.order, cell)
			}
			c.counts[cell]++
		}
	}
	return f
}

// doctypes are the doctypes in use, those with no schema first.
func (f *facets) doctypes(s *schema.Schema) []string {
	doctypes := make([]string, 0, len(f.seen))
	for dt := range f.seen {
		doctypes = append(doctypes, dt)
	}
	sort.Slice(doctypes, func(i, j int) bool {
		gi, gj := s.Types[doctypes[i]] != nil, s.Types[doctypes[j]] != nil
		if gi != gj {
			return !gi
		}
		return doctypes[i] < doctypes[j]
	})
	return doctypes
}

// print lists one doctype's keys, most used first, each with its six most used values.
func (f *facets) print(w io.Writer, s *schema.Schema, dt string) {
	seen := f.seen
	n := 0
	if c := seen[dt]["type"]; c != nil {
		n = c.total()
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
	keys := append([]string(nil), f.keyOrder[dt]...)
	sort.SliceStable(keys, func(i, j int) bool { return seen[dt][keys[i]].total() > seen[dt][keys[j]].total() })
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
		fmt.Fprintf(w, "  %s %s: %d/%d  %s%s\n", flag, k, c.total(), n, strings.Join(top, ", "), more)
	}
}

func segmentFacets(w io.Writer, s *schema.Schema, files []string) {
	if len(s.Segments) == 0 {
		return
	}
	c := New(s)
	used := map[*schema.Segment]map[string]bool{}
	for _, p := range files {
		for _, sg := range c.segments(p) {
			if used[sg.rule] == nil {
				used[sg.rule] = map[string]bool{}
			}
			used[sg.rule][sg.name] = true
		}
	}
	fmt.Fprintf(w, "\nsegments\n")
	for _, rule := range s.Segments {
		n := len(used[rule])
		if rule.Pattern != nil {
			fmt.Fprintf(w, "  %s  pattern %s: %d name(s) in use\n", rule.Path, rule.Pattern, n)
			continue
		}
		var idle []string
		for _, k := range schema.SortedKeys(rule.Values) {
			if !used[rule][k] {
				idle = append(idle, k)
			}
		}
		line := fmt.Sprintf("  %s  %s: %d name(s) in use", rule.Path, rule.Vocabulary, n)
		if len(idle) > 0 {
			line += fmt.Sprintf(", %d value(s) with no folder: %s", len(idle), strings.Join(idle, " · "))
		}
		fmt.Fprintln(w, line)
	}
}
