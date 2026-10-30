package check

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

// directory checks that a page sits in the folder its doctype's `directory:` names,
// with each `{field}` filled from its frontmatter, and that no other page under the
// pattern's fixed prefix shares its slug. A render names a page by slug alone, so an
// ambiguous slug would resolve to whichever folder it found first.
//
// A placeholder whose field is optional and absent drops out of the path, so
// `conventions/{kind}` puts a convention with no kind directly in conventions/.
func (c *Checker) directory(path string, fm *value.Mapping, pattern string, required []string, add adder) {
	var parts, fields []string
	for _, part := range strings.Split(pattern, "/") {
		names := schema.Placeholder.FindAllStringSubmatch(part, -1)
		if len(names) == 1 && names[0][0] == part && !present(fm, names[0][1]) && !slices.Contains(required, names[0][1]) {
			continue
		}
		for _, m := range names {
			if !present(fm, m[1]) {
				return // the `missing` fault for that field already says so
			}
			fields = append(fields, m[1])
		}
		parts = append(parts, schema.Placeholder.ReplaceAllStringFunc(part, func(ph string) string {
			v, _ := fm.Get(ph[1 : len(ph)-1])
			return value.Text(v)
		}))
	}
	want := filepath.Join(c.S.Base, filepath.FromSlash(strings.Join(parts, "/")))
	if got := filepath.Dir(path); got != want {
		label := strings.Join(fields, ", ")
		if label == "" {
			label = "directory"
		}
		add("value", label, nil, "the page is in "+schema.Display(got)+"/ but its frontmatter puts it in "+schema.Display(want)+"/")
	}
	stem := strings.TrimSuffix(filepath.Base(path), ".md")
	if stem == "index" {
		return
	}
	base := filepath.Join(c.S.Base, filepath.FromSlash(schema.DirectoryPrefix(pattern)))
	var twins []string
	for _, p := range c.pageStems(base)[stem] {
		if p != path {
			twins = append(twins, schema.Display(p))
		}
	}
	if len(twins) > 0 {
		add("value", "<slug>", stem, "also used by "+strings.Join(twins, ", ")+"; a slug is unique across "+schema.Display(base)+"/")
	}
}

// present is true when the page gives the field a value that can fill a path.
func present(fm *value.Mapping, field string) bool {
	v, ok := fm.Get(field)
	return ok && v != nil && value.Text(v) != ""
}

// pageStems indexes every page under dir, recursively, by its stem.
func (c *Checker) pageStems(dir string) map[string][]string {
	if out, ok := c.stems[dir]; ok {
		return out
	}
	out := map[string][]string{}
	for _, p := range walkPages(dir) {
		stem := strings.TrimSuffix(filepath.Base(p), ".md")
		out[stem] = append(out[stem], p)
	}
	c.stems[dir] = out
	return out
}

// walkPages returns every .md file under dir, recursively, as paths under dir even
// when dir itself is a symlink. A missing dir has none.
func walkPages(dir string) []string {
	out, _ := walk(dir, func(_ string, d os.DirEntry) bool { return strings.HasSuffix(d.Name(), ".md") })
	sortPaths(out)
	return out
}
