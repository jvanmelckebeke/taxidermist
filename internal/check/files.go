package check

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

// Files returns the files to check, always filtered to the governed roots. With no
// arguments it walks every root. Arguments are filtered the same way, so a caller
// never needs its own copy of the root list: a hook hands over every staged file and
// scope.yaml alone decides which of them the schema governs.
//
// A markdown file is always a candidate. A YAML file is one once some doctype has
// `files:`, unless it sits in the taxonomy directory, which describes the schema.
func Files(s *schema.Schema, args []string) ([]string, error) {
	roots, err := s.GovernedRoots()
	if err != nil {
		return nil, err
	}
	if len(args) > 0 {
		var out []string
		for _, a := range args {
			p, err := filepath.Abs(a)
			if err != nil || !candidate(s, p) {
				continue
			}
			if st, err := os.Stat(p); err != nil || st.IsDir() {
				continue
			}
			if under(p, roots) {
				out = append(out, p)
			}
		}
		return out, nil
	}
	var out []string
	for _, r := range roots {
		found, err := walk(r, func(p string, d os.DirEntry) bool {
			if !candidate(s, p) {
				return false
			}
			if d.Type()&os.ModeSymlink != 0 {
				if st, err := os.Stat(p); err != nil || st.IsDir() {
					return false
				}
			}
			return true
		})
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	sortPaths(out)
	return out, nil
}

// walk returns the files under dir, recursively, that keep accepts, as paths under
// dir even when dir itself is a symlink. Every walk of a tree goes through it.
func walk(dir string, keep func(p string, d os.DirEntry) bool) ([]string, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(real, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(real, p)
		if under := filepath.Join(dir, rel); keep(under, d) {
			out = append(out, under)
		}
		return nil
	})
	return out, err
}

// Governed returns the absolute path of a markdown or YAML file, which need not
// exist, or an error when no governed root holds it.
func Governed(s *schema.Schema, path string) (string, error) {
	roots, err := s.GovernedRoots()
	if err != nil {
		return "", err
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if schema.IsYAMLPath(p) && !s.HasYAML() {
		return "", fmt.Errorf("taxidermist: %s is a YAML file, and no doctype in types/ has `files:`", path)
	}
	if !candidate(s, p) || !under(p, roots) {
		return "", fmt.Errorf("taxidermist: %s is not a markdown or YAML file under a root scope.yaml governs", path)
	}
	return p, nil
}

// candidate is true for a file the check reads: markdown, or YAML once a doctype
// governs YAML and the file is not part of the taxonomy itself.
func candidate(s *schema.Schema, p string) bool {
	if strings.HasSuffix(p, ".md") {
		return true
	}
	return schema.IsYAMLPath(p) && s.HasYAML() && !under(p, []string{s.Dir})
}

func under(p string, roots []string) bool {
	for _, r := range roots {
		if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// sortPaths orders paths component by component, as Python sorts Path objects.
func sortPaths(ps []string) {
	sort.SliceStable(ps, func(i, j int) bool {
		a := strings.Split(ps[i], string(filepath.Separator))
		b := strings.Split(ps[j], string(filepath.Separator))
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
}
