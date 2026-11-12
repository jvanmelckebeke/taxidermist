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

// ConfigFile is the config's name, kept beside the schema folder.
const ConfigFile = ".taxidermist.yaml"

// legacyConfig is the pre-1.0 config, kept inside the schema folder.
const legacyConfig = "scope.yaml"

// DefaultDir is the schema folder when the config does not name one.
const DefaultDir = ".taxidermist"

type configFile struct {
	Taxonomy   string       `yaml:"taxonomy"`
	Docs       string       `yaml:"docs"`
	Roots      []string     `yaml:"roots"`
	Ungoverned string       `yaml:"ungoverned"`
	Invocation string       `yaml:"invocation"`
	Segments   []rawSegment `yaml:"segments"`
}

// Location says where a schema's folder and config are.
type Location struct {
	Dir    string // the schema folder
	Config string // the config file; "" when there is none
	Legacy bool   // Config is Dir/scope.yaml
}

// Locate finds the schema folder and its config. With dir set (--taxonomy) the config
// is .taxidermist.yaml in dir's parent, or the old dir/scope.yaml. With dir empty it
// looks in the working directory: .taxidermist.yaml, whose `taxonomy:` names the
// folder, or else taxonomy/scope.yaml. Both layouts at once is an error.
func Locate(dir string) (Location, error) {
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return Location{}, err
		}
		return locateAt(abs, filepath.Join(filepath.Dir(abs), ConfigFile))
	}
	wd, err := os.Getwd()
	if err != nil {
		return Location{}, err
	}
	cfg := filepath.Join(wd, ConfigFile)
	if exists(cfg) {
		var c configFile
		if err := readConfig(cfg, &c); err != nil {
			return Location{}, err
		}
		name := c.Taxonomy
		if name == "" {
			name = DefaultDir
		}
		return locateAt(filepath.Join(wd, name), cfg)
	}
	if exists(filepath.Join(wd, "taxonomy", legacyConfig)) {
		return locateAt(filepath.Join(wd, "taxonomy"), cfg)
	}
	return Location{}, errf("no %s in %s and no taxonomy/%s. Pass --taxonomy DIR.", ConfigFile, Display(wd), legacyConfig)
}

func locateAt(dir, cfg string) (Location, error) {
	old := filepath.Join(dir, legacyConfig)
	switch {
	case exists(cfg) && exists(old):
		return Location{}, errf("both %s and %s exist. Move the old layout across: "+
			"`git mv %s %s` and delete the other.", Display(cfg), Display(old), Display(old), Display(cfg))
	case exists(cfg):
		return Location{Dir: dir, Config: cfg}, nil
	case exists(old):
		return Location{Dir: dir, Config: old, Legacy: true}, nil
	}
	return Location{Dir: dir}, nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// readConfig decodes a config file. The new one rejects an unknown key; the old
// scope.yaml never did, so a 0.x repo keeps working.
func readConfig(path string, out *configFile) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var n yaml.Node
	if err := yaml.Unmarshal(raw, &n); err != nil {
		return errf("%s: %v", filepath.Base(path), err)
	}
	if n.Kind == 0 {
		return nil
	}
	if filepath.Base(path) == ConfigFile {
		if err := strict(n.Content[0], "the config", "taxonomy", "docs", "roots", "ungoverned", "invocation", "segments"); err != nil {
			return errf("%s: %v", ConfigFile, err)
		}
	}
	if err := n.Decode(out); err != nil {
		return errf("%s: %v", filepath.Base(path), err)
	}
	return nil
}

// loadConfig reads the config: the roots, where the docs go, how unknown doctypes are
// treated and how the repo runs taxidermist. It returns the segment rules, which bind
// once the vocabularies have loaded.
func (s *Schema) loadConfig(loc Location) ([]rawSegment, error) {
	var c configFile
	name := filepath.Base(loc.Config)
	if loc.Config != "" {
		if err := readConfig(loc.Config, &c); err != nil {
			return nil, err
		}
	}
	if loc.Config == "" {
		name = ConfigFile
	}
	s.ConfigName = name
	s.Legacy = loc.Legacy
	s.Config = loc.Config
	s.Base = filepath.Dir(loc.Dir)
	if loc.Config != "" && !loc.Legacy {
		s.Base = filepath.Dir(loc.Config)
	}
	s.DocsDir = filepath.Join(s.Dir, "format")
	if c.Docs != "" && !loc.Legacy {
		s.DocsDir = filepath.Join(s.Base, c.Docs)
	}
	s.Roots, s.Ungoverned = c.Roots, c.Ungoverned
	if s.Ungoverned == "" {
		s.Ungoverned = "error"
	}
	if s.Ungoverned != "error" && s.Ungoverned != "report" {
		return nil, errf("%s sets `ungoverned: %s`; it takes `error` or `report`.", name, s.Ungoverned)
	}
	s.Invocation = strings.TrimSpace(c.Invocation)
	if s.Invocation == "" {
		s.Invocation = "taxidermist"
	}
	if !strings.Contains(s.Invocation, "{command}") {
		s.Invocation += " {command}"
	}
	return c.Segments, nil
}

// checkDocsDir rejects a docs folder that is the schema folder, the config's folder or
// a governed root, since `docs` would then write over the files it sits among.
func (s *Schema) checkDocsDir() error {
	bad := s.DocsDir == s.Dir || s.DocsDir == s.Base
	for _, r := range s.Roots {
		bad = bad || s.DocsDir == filepath.Join(s.Base, r)
	}
	if bad {
		return errf("%s sets `docs: %s`, which is the schema folder, the config's folder or a governed root. Pick a folder of its own.",
			s.ConfigName, Display(s.DocsDir))
	}
	return nil
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

// loadSegments binds the config's segment rules.
func (s *Schema) loadSegments(segments []rawSegment) error {
	for i, r := range segments {
		seg, err := s.segment(r)
		if err != nil {
			return errf("%s segment %d: %v", s.ConfigName, i+1, err)
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

// GovernedRoots returns the absolute roots the config declares. A root is a directory,
// governed recursively, or a single file, which governs that file alone. A listed root
// that does not exist is a fault in the config, not a reason to quietly check less: a
// typo would otherwise drop a whole area from the gate and every commit would pass.
func (s *Schema) GovernedRoots() ([]string, error) {
	if len(s.Roots) == 0 {
		return nil, errf("%s declares no roots, so nothing would be checked. "+
			"List the directories or files this schema governs, relative to %s.", s.ConfigName, Display(s.Base))
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
		return nil, errf("%s lists %s, which do not exist under %s. Fix the path or drop the root.",
			s.ConfigName, strings.Join(missing, ", "), Display(s.Base))
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
