package check

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
	"go.yaml.in/yaml/v3"
)

// Facets prints every key and value in use per doctype, doctypes with no schema
// first. It is the drift report: what says `index` and `Index` are the same idea
// spelled twice, before anyone decides which one wins. A `!` marks a key the doctype
// does not declare. A YAML doctype counts its files, or for `shape: map` and
// `shape: list` its entries. Each segment rule then lists the values that have no folder yet,
// which is fine: a value can be defined before anything is filed under it.
func Facets(w io.Writer, s *schema.Schema, files []string) {
	f := tallyFacets(s, files)
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
	entries  map[string]int                 // a YAML doctype -> the mappings counted under it
}

func tallyFacets(s *schema.Schema, files []string) *facets {
	f := &facets{seen: map[string]map[string]*counter{}, keyOrder: map[string][]string{}, entries: map[string]int{}}
	for _, p := range files {
		if schema.IsYAMLPath(p) {
			f.tallyYAML(s, p)
			continue
		}
		fm, err := frontmatter.Read(p)
		if err != nil || fm == nil || len(fm.Keys) == 0 {
			continue
		}
		dtv, _ := fm.Get("type")
		f.tally(value.Text(dtv), fm)
	}
	return f
}

// tallyYAML counts a YAML file under the doctype its path takes, one mapping per
// entry or item for `shape: map` and `shape: list`. A file no doctype or two
// doctypes match is counted under <yaml> as a single mapping.
func (f *facets) tallyYAML(s *schema.Schema, path string) {
	raw, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(raw) {
		return
	}
	var root yaml.Node
	if yaml.Unmarshal(raw, &root) != nil || len(root.Content) == 0 {
		return
	}
	top := root.Content[0]
	dt, shape := NoFilesMatch, "fields"
	rel, _ := filepath.Rel(s.Base, path)
	if types := s.TypesOf(filepath.ToSlash(rel)); len(types) == 1 {
		dt, shape = types[0].Doctype, types[0].Shape
	}
	var maps []*yaml.Node
	switch {
	case shape == "map" && top.Kind == yaml.MappingNode:
		for i := 1; i < len(top.Content); i += 2 {
			maps = append(maps, top.Content[i])
		}
	case shape == "list" && top.Kind == yaml.SequenceNode:
		maps = top.Content
	case shape == "fields":
		maps = []*yaml.Node{top}
	}
	for _, n := range maps {
		if n.Kind != yaml.MappingNode {
			continue
		}
		if m, err := value.MappingOf(n); err == nil && len(m.Keys) > 0 {
			f.tally(dt, m)
			f.entries[dt]++
		}
	}
}

// tally counts one mapping's keys and values under a doctype.
func (f *facets) tally(dt string, fm *value.Mapping) {
	if f.seen[dt] == nil {
		f.seen[dt] = map[string]*counter{}
	}
	for _, key := range fm.Keys {
		v := fm.Fields[key]
		// A block scalar keeps to its one line of the report.
		cell := value.Truncate(strings.ReplaceAll(value.Text(v), "\n", `\n`), 40)
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
	n, noun := 0, "files"
	if c := seen[dt]["type"]; c != nil {
		n = c.total()
	}
	t := s.Types[dt]
	if (t != nil && t.IsYAML()) || dt == NoFilesMatch {
		n = f.entries[dt]
		if t != nil && t.Shape != "fields" {
			noun = "entries"
		}
	}
	mark := ""
	if t == nil {
		mark = "   [no schema]"
	}
	fmt.Fprintf(w, "\n%s  (%d %s)%s\n", dt, n, noun, mark)
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
