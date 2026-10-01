// Package frontmatter extracts and parses the YAML block at the top of a markdown file.
package frontmatter

import (
	"errors"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// ErrUnclosed is a block that opens with `---` and never closes.
var ErrUnclosed = errors.New("opens a frontmatter block that is never closed (no closing ---)")

// Block returns the YAML text between an opening `---` line and the next `---` line.
// opened is false when the file does not start a block at all.
func Block(text string) (block string, opened bool, err error) {
	first, rest, found := strings.Cut(text, "\n")
	if strings.TrimRight(first, " \t\r") != "---" {
		return "", false, nil
	}
	if !found {
		return "", true, ErrUnclosed
	}
	var lines []string
	for {
		line, next, more := strings.Cut(rest, "\n")
		if strings.TrimRight(line, " \t\r") == "---" {
			return strings.Join(lines, "\n"), true, nil
		}
		if !more {
			return "", true, ErrUnclosed
		}
		lines = append(lines, strings.TrimSuffix(line, "\r"))
		rest = next
	}
}

// Doc is a parsed frontmatter mapping. Keys holds the top-level keys in the order
// the file writes them.
type Doc struct {
	Keys   []string
	Fields map[string]any
}

func (d *Doc) Get(key string) (any, bool) {
	v, ok := d.Fields[key]
	return v, ok
}

// Parse reads a block of YAML. It returns nil for an empty block or one that holds
// something other than a mapping.
func Parse(block string) (*Doc, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(block), &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	m := root.Content[0]
	d := &Doc{}
	if err := m.Decode(&d.Fields); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i].Value
		if _, ok := d.Fields[k]; ok && !seen[k] {
			seen[k] = true
			d.Keys = append(d.Keys, k)
		}
	}
	// Keys that arrived through a `<<` merge, after the ones written out.
	var merged []string
	for k := range d.Fields {
		if !seen[k] {
			merged = append(merged, k)
		}
	}
	sort.Strings(merged)
	d.Keys = append(d.Keys, merged...)
	return d, nil
}

// Read parses a file's frontmatter. It returns nil when the file has no block, is not
// UTF-8, or holds no mapping. A block that opens and does not parse is an error: the
// file looks fine on disk and is invisible to every tool that reads it.
func Read(path string) (*Doc, error) {
	raw, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(raw) {
		return nil, nil
	}
	block, opened, err := Block(string(raw))
	if !opened {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(block)
}
