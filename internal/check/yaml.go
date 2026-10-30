package check

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

// Ungoverned YAML files are tallied under this name when scope.yaml says
// `ungoverned: report`, since they have no doctype to tally under.
const NoFilesMatch = "<yaml>"

// yamlFile checks a YAML file against the one doctype whose `files:` glob matches
// its path. The doctype's `shape:` says what its top level is: a field set, a map
// of entries that each take the field set, or a list of such items.
func (c *Checker) yamlFile(path string, raw []byte, r *Result, add adder) {
	rel := c.rel(path)
	types := c.S.TypesOf(rel)
	switch {
	case len(types) == 0:
		if c.S.Ungoverned == "report" {
			r.Ungoverned[NoFilesMatch] = append(r.Ungoverned[NoFilesMatch], path)
			return
		}
		add("value", "<file>", nil, "no doctype's `files:` matches "+rel+". Known: "+strings.Join(c.globs(), " · "))
		return
	case len(types) > 1:
		var names []string
		for _, t := range types {
			names = append(names, "types/"+t.Slug+".yaml")
		}
		add("value", "<file>", nil, "matched by the `files:` of "+joinAnd(names)+"; a file has one doctype")
		return
	}
	t := types[0]
	if !utf8.Valid(raw) {
		add("parse", "<file>", nil, "the file is not UTF-8")
		return
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		add("parse", "<file>", nil, "the file does not parse, so every reader of it fails: "+firstLine(err.Error()))
		return
	}
	var top *yaml.Node
	if len(root.Content) > 0 {
		top = root.Content[0]
	}
	skip := c.exemptions(path)
	switch t.Shape {
	case "map":
		if !c.kindIs(top, yaml.MappingNode, "a mapping of entries (`shape: map`)", add) {
			return
		}
		for i := 0; i+1 < len(top.Content); i += 2 {
			k := top.Content[i].Value
			if t.Key != nil {
				c.element(k, k, t.Key, add)
			}
			c.entry(k, top.Content[i+1], t, skip, add)
		}
	case "list":
		if !c.kindIs(top, yaml.SequenceNode, "a list of items (`shape: list`)", add) {
			return
		}
		for i, item := range top.Content {
			c.entry("["+strconv.Itoa(i)+"]", item, t, skip, add)
		}
	default:
		if !c.kindIs(top, yaml.MappingNode, "a mapping of fields", add) {
			return
		}
		c.entry("", top, t, skip, add)
	}
}

// kindIs is true when n is a node of the kind, or empty. Otherwise it adds a fault
// saying what the doctype's shape wants.
func (c *Checker) kindIs(n *yaml.Node, kind yaml.Kind, want string, add adder) bool {
	if isNull(n) || n.Kind == kind {
		return true
	}
	add("type", "<file>", nil, "the top level is "+kindName(n)+"; the doctype wants "+want)
	return false
}

func kindName(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		return "a mapping"
	case yaml.SequenceNode:
		return "a list"
	}
	var got any
	_ = n.Decode(&got)
	return "a " + value.TypeName(got)
}

// entry checks one mapping against the doctype's field set. label names it in a
// fault: an entry's key, an item's index, or nothing for the whole file.
func (c *Checker) entry(label string, n *yaml.Node, t *schema.Type, skip map[string]bool, add adder) {
	prefix := ""
	if label != "" {
		prefix = label + "."
	}
	if isNull(n) {
		c.fieldSet(prefix, &frontmatter.Doc{Fields: map[string]any{}}, t, skip, add)
		return
	}
	if n.Kind != yaml.MappingNode {
		var got any
		_ = n.Decode(&got)
		add("type", label, got, "expected a mapping of `"+t.Doctype+"`'s fields, got "+value.TypeName(got))
		return
	}
	doc, err := frontmatter.FromNode(n)
	if err != nil {
		add("parse", label, nil, firstLine(err.Error()))
		return
	}
	c.fieldSet(prefix, doc, t, skip, add)
}

// isNull is true for an empty file or a key with no value.
func isNull(n *yaml.Node) bool {
	return n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}

// rel is a path relative to the taxonomy's parent, with slashes.
func (c *Checker) rel(path string) string {
	rel, err := filepath.Rel(c.S.Base, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// globs lists every YAML doctype's `files:`.
func (c *Checker) globs() []string {
	var out []string
	for _, dt := range c.S.Doctypes() {
		if t := c.S.Types[dt]; t.IsYAML() {
			out = append(out, t.Files)
		}
	}
	return out
}

func joinAnd(xs []string) string {
	if len(xs) < 2 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}
