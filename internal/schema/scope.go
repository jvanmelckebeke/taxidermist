package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Segment ties the names directly under a directory to a vocabulary or a pattern:
// the first path component below Dir, a folder name or the stem of a page sitting
// directly there, must be an allowed value.
type Segment struct {
	Path       string // as written, ending in `/*`
	Dir        string // Path without the `/*`, relative to Base
	Vocabulary string
	Pattern    *regexp.Regexp
	Field      string // a frontmatter field expected to agree with the folder; disagreeing warns
	Values     []Def  // the vocabulary's definitions; nil for a pattern
}

// loadScope reads scope.yaml: the roots, how it treats unknown doctypes and how the
// repo runs taxidermist. It returns the segment rules, which bind once the
// vocabularies have loaded.
func (s *Schema) loadScope() ([]rawSegment, error) {
	var scope struct {
		Roots      []string     `yaml:"roots"`
		Ungoverned string       `yaml:"ungoverned"`
		Invocation string       `yaml:"invocation"`
		Segments   []rawSegment `yaml:"segments"`
	}
	if err := read(s.Dir, "scope.yaml", &scope); err != nil {
		return nil, err
	}
	s.Roots, s.Ungoverned = scope.Roots, scope.Ungoverned
	if s.Ungoverned == "" {
		s.Ungoverned = "error"
	}
	if s.Ungoverned != "error" && s.Ungoverned != "report" {
		return nil, errf("scope.yaml sets `ungoverned: %s`; it takes `error` or `report`.", s.Ungoverned)
	}
	s.Invocation = strings.TrimSpace(scope.Invocation)
	if s.Invocation == "" {
		s.Invocation = "taxidermist"
	}
	if !strings.Contains(s.Invocation, "{command}") {
		s.Invocation += " {command}"
	}
	return scope.Segments, nil
}

// loadExempt reads exempt.yaml.
func (s *Schema) loadExempt() error {
	var exempt struct {
		Exempt []Exemption `yaml:"exempt"`
	}
	if err := read(s.Dir, "exempt.yaml", &exempt); err != nil {
		return err
	}
	for i, e := range exempt.Exempt {
		if e.Paths == "" {
			return errf("exempt.yaml entry %d has no `paths:` glob.", i+1)
		}
	}
	s.Exempt = exempt.Exempt
	return nil
}

// loadSegments binds scope.yaml's segment rules.
func (s *Schema) loadSegments(segments []rawSegment) error {
	for i, r := range segments {
		seg, err := s.segment(r)
		if err != nil {
			return errf("scope.yaml segment %d: %v", i+1, err)
		}
		s.Segments = append(s.Segments, seg)
	}
	return nil
}

type rawSegment struct {
	Path       string `yaml:"path"`
	Vocabulary string `yaml:"vocabulary"`
	Pattern    string `yaml:"pattern"`
	Field      string `yaml:"field"`
}

func (r *rawSegment) UnmarshalYAML(n *yaml.Node) error {
	if err := strict(n, "a segment", "path", "vocabulary", "pattern", "field"); err != nil {
		return err
	}
	type plain rawSegment
	return n.Decode((*plain)(r))
}

type Exemption struct {
	Paths string   `yaml:"paths"`
	Omit  []string `yaml:"omit"`
	Why   string   `yaml:"why"`
}

func (s *Schema) segment(r rawSegment) (*Segment, error) {
	dir, ok := strings.CutSuffix(r.Path, "/*")
	if !ok || dir == "" || strings.ContainsAny(dir, "*?[") {
		return nil, fmt.Errorf("`path: %s` must be a directory followed by `/*`, with no other wildcard", r.Path)
	}
	seg := &Segment{Path: r.Path, Dir: filepath.ToSlash(filepath.Clean(dir)), Vocabulary: r.Vocabulary, Field: r.Field}
	switch {
	case (r.Vocabulary == "") == (r.Pattern == ""):
		return nil, fmt.Errorf("`%s` needs exactly one of `vocabulary:` and `pattern:`", r.Path)
	case r.Vocabulary != "":
		v, ok := s.Vocabularies[r.Vocabulary]
		if !ok {
			return nil, fmt.Errorf("`%s` names vocabulary `%s`, and vocabularies/%s.yaml does not exist", r.Path, r.Vocabulary, r.Vocabulary)
		}
		seg.Values = v.Defs
	default:
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("`%s` pattern: %v", r.Path, err)
		}
		seg.Pattern = re
	}
	return seg, nil
}

// GovernedRoots returns the absolute roots scope.yaml declares. A root is a directory,
// governed recursively, or a single file, which governs that file alone. A listed root
// that does not exist is a fault in scope.yaml, not a reason to quietly check less: a
// typo would otherwise drop a whole area from the gate and every commit would pass.
func (s *Schema) GovernedRoots() ([]string, error) {
	if len(s.Roots) == 0 {
		return nil, errf("scope.yaml declares no roots, so nothing would be checked. "+
			"List the directories or files this schema governs, relative to %s.", Display(s.Base))
	}
	var out, missing []string
	for _, r := range s.Roots {
		p := filepath.Clean(filepath.Join(s.Base, r))
		if _, err := os.Stat(p); err != nil {
			missing = append(missing, r)
			continue
		}
		out = append(out, p)
	}
	if len(missing) > 0 {
		return nil, errf("scope.yaml lists %s, which do not exist under %s. Fix the path or drop the root.",
			strings.Join(missing, ", "), Display(s.Base))
	}
	return out, nil
}

// Display shows a path relative to the working directory when it sits under it.
func Display(p string) string {
	wd, err := os.Getwd()
	if err != nil {
		return p
	}
	rel, err := filepath.Rel(wd, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return filepath.ToSlash(rel)
}
