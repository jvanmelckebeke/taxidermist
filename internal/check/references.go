package check

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

// references checks that every value is the slug of a page anywhere under the
// directory the field names, so `reference: collections` reaches every kind's
// subfolder. A `<dir>/*` reference names directories instead: the value is a folder
// directly under <dir> that holds an index.md, and the pages inside it are not values.
// A reference to a YAML file takes that file's top-level keys. With `reference_type:`,
// only a page whose own `type:` is that doctype counts, read from index.md for `<dir>/*`.
func (c *Checker) references(label string, v any, spec *schema.Field, add adder) {
	ref := spec.Reference
	pages := c.pages(ref)
	dir, nested := strings.CutSuffix(ref, "/*")
	shown := schema.Display(filepath.Join(c.S.Base, dir))
	for _, x := range each(v) {
		if x == nil {
			continue
		}
		slug, member, pointed := value.Text(x), "", false
		if spec.Members != "" {
			slug, member, pointed = strings.Cut(slug, "::")
		}
		if pages[slug] {
			if spec.RefType != "" && !c.referenceType(label, slug, x, spec, add) {
				continue
			}
			if pointed {
				c.member(label, slug, member, x, spec, add)
			}
			continue
		}
		var why string
		switch {
		case schema.IsYAMLPath(ref):
			why = "not a top-level key of " + shown
			if _, err := os.Stat(filepath.Join(c.S.Base, ref)); err != nil {
				why += ", which does not exist"
			} else {
				why += ", which has " + strconv.Itoa(len(pages)) + " keys"
			}
			add("value", label, x, why)
			continue
		case nested:
			why = "no page at " + shown + "/" + slug + "/index.md"
		default:
			why = "no page at " + shown + "/" + slug + ".md"
		}
		noun := "pages"
		if nested {
			noun = "directories with an index.md"
		}
		if len(pages) > 0 {
			why += ". " + shown + "/ has " + strconv.Itoa(len(pages)) + " " + noun
		} else {
			why += ". " + shown + "/ holds no " + noun
		}
		add("value", label, x, why)
	}
}

// referenceType checks that a page the value names declares the field's
// `reference_type:`, and reports whether it does. Two pages may share a stem under
// the referenced directory, and one of the right type is enough.
func (c *Checker) referenceType(label, slug string, x any, spec *schema.Field, add adder) bool {
	types := c.pageTypes(spec.Reference)[slug]
	if slices.Contains(types, spec.RefType) {
		return true
	}
	var shown []string
	for _, t := range types {
		if t == "" {
			t = "no type"
		}
		shown = append(shown, "`"+t+"`")
	}
	add("value", label, x, "the page is "+strings.Join(shown, ", ")+"; this field takes only `"+spec.RefType+"` pages")
	return false
}

// member checks the part of `<slug>::<member>` after the separator against what the
// page lists under the field's `members:`. Two pages may share a stem, and a member
// of either counts.
func (c *Checker) member(label, slug, member string, x any, spec *schema.Field, add adder) {
	got := c.pageMembers(spec.Reference, spec.Members)[slug]
	if slices.Contains(got, member) {
		return
	}
	why := "`" + member + "` is not listed under `" + spec.Members + "` on page `" + slug + "`"
	if len(got) > 0 {
		why += ". It lists " + strings.Join(got, " · ")
	} else {
		why += ", which lists none"
	}
	add("value", label, x, why)
}

// eachPage calls fn with the slug and path of every page a reference names.
func (c *Checker) eachPage(ref string, fn func(slug, path string)) {
	if dir, nested := strings.CutSuffix(ref, "/*"); nested {
		root := filepath.Join(c.S.Base, dir)
		for name := range c.pages(ref) {
			fn(name, filepath.Join(root, name, "index.md"))
		}
		return
	}
	for _, p := range walkPages(filepath.Join(c.S.Base, ref)) {
		if stem := strings.TrimSuffix(filepath.Base(p), ".md"); stem != "index" {
			fn(stem, p)
		}
	}
}

// pageMembers maps each slug of a page reference to the scalars listed under field in
// the frontmatter of every page with that slug. A page without the field lists none.
func (c *Checker) pageMembers(ref, field string) map[string][]string {
	key := ref + "\x00" + field
	if out, ok := c.members[key]; ok {
		return out
	}
	out := map[string][]string{}
	c.eachPage(ref, func(slug, p string) {
		raw, _ := os.ReadFile(p)
		fm, err := frontmatter.Bytes(raw)
		if err != nil || fm == nil {
			return
		}
		v, ok := fm.Get(field)
		if !ok {
			return
		}
		for _, m := range each(v) {
			if m != nil && value.IsScalar(m) {
				out[slug] = append(out[slug], value.Text(m))
			}
		}
	})
	c.members[key] = out
	return out
}

// pageTypes maps each value of a page reference to the `type:` of every page it names.
// A page with no readable `type:` maps to "".
func (c *Checker) pageTypes(ref string) map[string][]string {
	if out, ok := c.types[ref]; ok {
		return out
	}
	out := map[string][]string{}
	read := func(name, p string) {
		raw, _ := os.ReadFile(p)
		t := ""
		if fm, err := frontmatter.Bytes(raw); err == nil && fm != nil {
			if v, ok := fm.Get("type"); ok && value.IsScalar(v) {
				t = value.Text(v)
			}
		}
		out[name] = append(out[name], t)
	}
	c.eachPage(ref, read)
	c.types[ref] = out
	return out
}

func (c *Checker) pages(ref string) map[string]bool {
	if p, ok := c.refs[ref]; ok {
		return p
	}
	out := map[string]bool{}
	if schema.IsYAMLPath(ref) {
		raw, _ := os.ReadFile(filepath.Join(c.S.Base, ref))
		var root yaml.Node
		if yaml.Unmarshal(raw, &root) == nil && len(root.Content) > 0 && root.Content[0].Kind == yaml.MappingNode {
			m := root.Content[0]
			for i := 0; i+1 < len(m.Content); i += 2 {
				out[m.Content[i].Value] = true
			}
		}
	} else if dir, nested := strings.CutSuffix(ref, "/*"); nested {
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
