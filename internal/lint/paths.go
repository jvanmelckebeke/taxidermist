package lint

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/check"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

// segmentRules: a segment rule's directory exists under a governed root, and its field
// is one some doctype declares.
func segmentRules(s *schema.Schema, add adder) {
	for _, sg := range s.Segments {
		dir := filepath.Join(s.Base, filepath.FromSlash(sg.Dir))
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			add("segment-path", "segment `%s`: %s does not exist", sg.Path, sg.Dir)
		} else if !underRoot(s, sg.Dir) {
			add("segment-path", "segment `%s` is under no root in scope.yaml, so check never reaches it", sg.Path)
		}
		if sg.Field != "" && len(s.Owners[sg.Field]) == 0 {
			add("segment-field", "segment `%s` names field `%s`, which no doctype declares", sg.Path, sg.Field)
		}
	}
}

// filesUnderRoot: a YAML doctype's files: glob lies under a governed root.
func filesUnderRoot(s *schema.Schema, add adder) {
	doctypes := s.Doctypes()
	for _, dt := range doctypes {
		t := s.Types[dt]
		if t.IsYAML() && !underRoot(s, globDir(t.Files)) {
			add("files-under-root", "`%s`: `files: %s` is under no root in scope.yaml, so check never reaches it", dt, t.Files)
		}
	}
}

// filesOneType: no YAML file under a governed root matches two doctypes' files:.
func filesOneType(s *schema.Schema, add adder) {
	if s.HasYAML() {
		files, err := check.Files(s, nil)
		if err != nil {
			add("files-one-type", "%v", err)
		}
		for _, p := range files {
			if !schema.IsYAMLPath(p) {
				continue
			}
			rel, _ := filepath.Rel(s.Base, p)
			types := s.TypesOf(filepath.ToSlash(rel))
			if len(types) < 2 {
				continue
			}
			var names []string
			for _, t := range types {
				names = append(names, "`"+t.Doctype+"`")
			}
			add("files-one-type", "%s matches the files: of %s; a file has one doctype", schema.Display(p), strings.Join(names, " and "))
		}
	}
}

// globDir is the directory part of a path glob before its first wildcard.
func globDir(glob string) string {
	parts := strings.Split(glob, "/")
	var fixed []string
	for _, p := range parts[:len(parts)-1] {
		if strings.ContainsAny(p, "*?[") {
			break
		}
		fixed = append(fixed, p)
	}
	return strings.Join(fixed, "/")
}

func underRoot(s *schema.Schema, dir string) bool {
	for _, r := range s.Roots {
		r = filepath.ToSlash(filepath.Clean(r))
		if dir == r || strings.HasPrefix(dir, r+"/") {
			return true
		}
	}
	return false
}
