package check

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

// references checks that every value is the slug of a page anywhere under the
// directory the field names, so `reference: collections` reaches every kind's
// subfolder. A `<dir>/*` reference names directories instead: the value is a folder
// directly under <dir> that holds an index.md, and the pages inside it are not values.
// A reference to a YAML file takes that file's top-level keys.
func (c *Checker) references(label string, v any, ref string, add adder) {
	pages := c.pages(ref)
	dir, nested := strings.CutSuffix(ref, "/*")
	shown := schema.Display(filepath.Join(c.S.Base, dir))
	for _, x := range each(v) {
		if x == nil || pages[value.Text(x)] {
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
			why = "no page at " + shown + "/" + value.Text(x) + "/index.md"
		default:
			why = "no page at " + shown + "/" + value.Text(x) + ".md"
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
