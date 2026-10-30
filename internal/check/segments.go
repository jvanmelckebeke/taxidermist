package check

import (
	"path/filepath"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

// placed is a file's place under one segment rule: the name it sits under.
type placed struct {
	rule *schema.Segment
	name string
}

func (p placed) dir(s *schema.Schema) string {
	return filepath.Join(s.Base, filepath.FromSlash(p.rule.Dir), p.name)
}

// segments returns, for each rule whose directory holds the file, the first path
// component below that directory: a folder name, or the stem of a page sitting
// directly there. index.md and README.md directly in the directory describe the
// directory itself and name no segment.
func (c *Checker) segments(path string) []placed {
	rel, err := filepath.Rel(c.S.Base, path)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	var out []placed
	for _, rule := range c.S.Segments {
		rest, ok := strings.CutPrefix(rel, rule.Dir+"/")
		if !ok {
			continue
		}
		name, _, nested := strings.Cut(rest, "/")
		if !nested {
			name = strings.TrimSuffix(name, ".md")
			if name == "index" || name == "README" {
				continue
			}
		}
		out = append(out, placed{rule, name})
	}
	return out
}

func (c *Checker) segmentFault(rule *schema.Segment, name string) string {
	if rule.Pattern != nil {
		if rule.Pattern.MatchString(name) {
			return ""
		}
		return "the name does not match `" + rule.Pattern.String() + "`"
	}
	for _, d := range rule.Values {
		if d.Key == name {
			return ""
		}
	}
	return "not a value of `" + rule.Vocabulary + "`, so the folder names nothing the vocabulary defines. Allowed: " +
		strings.Join(schema.SortedKeys(rule.Values), " · ")
}

// hasText is true when v, or any item of a list v, is written as want.
func hasText(v any, want string) bool {
	for _, x := range each(v) {
		if value.IsScalar(x) && value.Text(x) == want {
			return true
		}
	}
	return false
}

// segmentFaults adds a fault for each segment rule whose name the file sits under is
// not an allowed value.
func (c *Checker) segmentFaults(path string, segs []placed, r *Result) {
	for _, sg := range segs {
		if why := c.segmentFault(sg.rule, sg.name); why != "" {
			r.Faults = append(r.Faults, Fault{Path: path, Kind: "segment", Field: sg.rule.Path, Value: sg.name, Why: why, Dir: sg.dir(c.S)})
		}
	}
}

// mismatches warns where a segment rule names a field and the page's value for it
// differs from the folder it sits in.
func (c *Checker) mismatches(path string, segs []placed, fm *value.Mapping, r *Result) {
	for _, sg := range segs {
		if sg.rule.Field == "" {
			continue
		}
		v, ok := fm.Get(sg.rule.Field)
		if !ok || hasText(v, sg.name) {
			continue
		}
		r.Warnings = append(r.Warnings, Fault{Path: path, Kind: "mismatch", Field: sg.rule.Field, Value: v,
			Why: "the folder says `" + sg.name + "` (" + sg.rule.Path + ")", Dir: sg.dir(c.S)})
	}
}
