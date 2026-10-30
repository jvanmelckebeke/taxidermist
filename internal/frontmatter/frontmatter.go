// Package frontmatter extracts and parses the YAML block at the top of a markdown file.
package frontmatter

import (
	"errors"
	"os"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/jvanmelckebeke/taxidermist/internal/value"
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

// Parse reads a block of YAML. It returns nil for an empty block or one that holds
// something other than a mapping.
func Parse(block string) (*value.Mapping, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(block), &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	return value.MappingOf(root.Content[0])
}

// Read parses a file's frontmatter. It returns nil when the file has no block, is not
// UTF-8, or holds no mapping. A block that opens and does not parse is an error: the
// file looks fine on disk and is invisible to every tool that reads it.
func Read(path string) (*value.Mapping, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	return Bytes(raw)
}

// Bytes parses the frontmatter of a file's content, as Read does.
func Bytes(raw []byte) (*value.Mapping, error) {
	if !utf8.Valid(raw) {
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
