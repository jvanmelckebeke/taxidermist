package check

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

// Files returns the files to check, always filtered to the governed roots. With no
// arguments it walks every root. Arguments are filtered the same way, so a caller
// never needs its own copy of the root list: a hook hands over every staged markdown
// file and scope.yaml alone decides which of them the schema governs.
func Files(s *schema.Schema, args []string) ([]string, error) {
	roots, err := s.GovernedRoots()
	if err != nil {
		return nil, err
	}
	if len(args) > 0 {
		var out []string
		for _, a := range args {
			if !strings.HasSuffix(a, ".md") {
				continue
			}
			p, err := filepath.Abs(a)
			if err != nil {
				continue
			}
			if st, err := os.Stat(p); err != nil || st.IsDir() {
				continue
			}
			for _, r := range roots {
				if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
					out = append(out, p)
					break
				}
			}
		}
		return out, nil
	}
	var out []string
	for _, r := range roots {
		real, err := filepath.EvalSymlinks(r)
		if err != nil {
			return nil, err
		}
		err = filepath.WalkDir(real, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				if st, err := os.Stat(p); err != nil || st.IsDir() {
					return nil
				}
			}
			rel, _ := filepath.Rel(real, p)
			out = append(out, filepath.Join(r, rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sortPaths(out)
	return out, nil
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
