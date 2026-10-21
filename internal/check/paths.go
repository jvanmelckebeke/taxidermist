package check

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
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
func (c *Checker) directory(path string, fm *frontmatter.Doc, pattern string, required []string, add adder) {
	var parts, fields, prefix []string
	fixed := true
	for _, part := range strings.Split(pattern, "/") {
		names := schema.Placeholder.FindAllStringSubmatch(part, -1)
		if len(names) > 0 {
			fixed = false
		} else if fixed {
			prefix = append(prefix, part)
		}
		if len(names) == 1 && names[0][0] == part && !present(fm, names[0][1]) && !contains(required, names[0][1]) {
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
	base := filepath.Join(c.S.Base, filepath.FromSlash(strings.Join(prefix, "/")))
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
func present(fm *frontmatter.Doc, field string) bool {
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
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}
	var out []string
	filepath.WalkDir(real, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, _ := filepath.Rel(real, p)
		out = append(out, filepath.Join(dir, rel))
		return nil
	})
	sortPaths(out)
	return out
}

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
		strings.Join(sortedKeys(rule.Values), " · ")
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

// references checks that every value is the slug of a page anywhere under the
// directory the field names, so `reference: collections` reaches every kind's
// subfolder. A `<dir>/*` reference names directories instead: the value is a folder
// directly under <dir> that holds an index.md, and the pages inside it are not values.
func (c *Checker) references(label string, v any, ref string, add adder) {
	pages := c.pages(ref)
	dir, nested := strings.CutSuffix(ref, "/*")
	shown := schema.Display(filepath.Join(c.S.Base, dir))
	noun := "pages"
	if nested {
		noun = "directories with an index.md"
	}
	for _, x := range each(v) {
		if x == nil || pages[value.Text(x)] {
			continue
		}
		why := "no page at " + shown + "/" + value.Text(x) + ".md"
		if nested {
			why = "no page at " + shown + "/" + value.Text(x) + "/index.md"
		}
		if len(pages) > 0 {
			why += ". " + shown + "/ has " + strconv.Itoa(len(pages)) + " " + noun
		} else {
			why += ". " + shown + "/ holds no " + noun
		}
		add("value", label, x, why)
	}
}

func (c *Checker) pages(ref string) map[string]bool {
	if p, ok := c.refs[ref]; ok {
		return p
	}
	out := map[string]bool{}
	if dir, nested := strings.CutSuffix(ref, "/*"); nested {
		root := filepath.Join(c.S.Base, dir)
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if st, err := os.Stat(filepath.Join(root, e.Name(), "index.md")); err == nil && !st.IsDir() {
				out[e.Name()] = true
			}
		}
	} else {
		for _, p := range walkPages(filepath.Join(c.S.Base, ref)) {
			if stem := strings.TrimSuffix(filepath.Base(p), ".md"); stem != "index" {
				out[stem] = true
			}
		}
	}
	c.refs[ref] = out
	return out
}
