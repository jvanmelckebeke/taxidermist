// Package frontmatter extracts and parses the YAML block at the top of a markdown file.
package frontmatter

import (
	"errors"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/jvanmelckebeke/taxidermist/internal/pyyaml"
)

// ErrUnclosed is a block that opens with `---` and never closes.
var ErrUnclosed = errors.New("opens a frontmatter block that is never closed (no closing ---)")

// Block returns the YAML text between an opening `---` line and the next `---` line.
// ok is false when the file does not open a block at all.
func Block(text string) (block string, ok bool, err error) {
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

// Read parses a file's frontmatter. It returns a Map, or Null when the file has no
// block, is not UTF-8, or holds something other than a mapping. A block that opens
// and does not parse is an error: the file looks fine on disk and is invisible to
// every tool that reads it.
func Read(path string) (pyyaml.Value, error) {
	raw, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(raw) {
		return pyyaml.Value{}, nil
	}
	block, ok, err := Block(string(raw))
	if !ok {
		return pyyaml.Value{}, nil
	}
	if err != nil {
		return pyyaml.Value{}, err
	}
	v, err := pyyaml.Parse([]byte(block))
	if err != nil {
		return pyyaml.Value{}, err
	}
	if v.Kind != pyyaml.Map {
		return pyyaml.Value{}, nil
	}
	return v, nil
}
