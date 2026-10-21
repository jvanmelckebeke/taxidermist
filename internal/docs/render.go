package docs

import (
	"slices"
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

func valueType(f *schema.Field) string {
	if f == nil || f.Value == "" {
		return "str"
	}
	return f.Value
}

// nested is the key block of an object field or of a list's items, whichever it has.
func nested(f *schema.Field) []*schema.Field {
	return f.Nested()
}

func render(s *schema.Schema, t *schema.Type) string {
	names := append(append([]string(nil), t.Required...), t.Optional...)
	required := map[string]bool{}
	for _, n := range t.Required {
		required[n] = true
	}
	spec := func(name string) *schema.Field {
		if f := t.Resolved[name]; f != nil {
			return f
		}
		return &schema.Field{Name: name}
	}
	vals := func(f *schema.Field) []string {
		seen := map[string]bool{}
		var out []string
		for _, d := range f.Enum() {
			k := d.Key
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
		sort.Strings(out)
		return out
	}

	L := []string{"# Frontmatter for `type: " + t.Doctype + "`", "", banner(s), ""}
	if len(t.Traits) > 0 {
		quoted := make([]string, len(t.Traits))
		for i, tr := range t.Traits {
			quoted[i] = "`" + tr + "`"
		}
		L = append(L, "Composes "+joinAnd(quoted)+", the field bundles in [../traits.yaml](../traits.yaml).", "")
	}

	L = append(L, "## Copy this", "", "```yaml", "---")
	for _, name := range names {
		f, v := spec(name), vals(spec(name))
		tail := ""
		if !required[name] {
			tail = "   # optional"
		}
		sub := nested(f)
		switch {
		case name == "type":
			L = append(L, "type: "+t.Doctype)
		case len(sub) > 0 && f.Value == "object":
			L = append(L, name+":"+tail)
			for _, k := range sub {
				L = append(L, "  "+k.Name+": <"+k.Value+">"+optMark(k))
			}
		case len(sub) > 0:
			L = append(L, name+":"+tail)
			for i, k := range sub {
				lead := "    "
				if i == 0 {
					lead = "  - "
				}
				L = append(L, lead+k.Name+": <"+k.Value+">"+optMark(k))
			}
		case len(v) > 0 && f.Value == "list":
			L = append(L, name+": [<"+strings.Join(v, " | ")+">]"+tail)
		case len(v) > 0:
			L = append(L, name+": <"+strings.Join(v, " | ")+">"+tail)
		case f.Value == "list":
			L = append(L, name+": []"+tail)
		default:
			L = append(L, name+": <"+valueType(f)+">"+tail)
		}
	}
	L = append(L, "---", "```", "")

	for _, c := range t.RequiredWhen {
		var parts []string
		for i, w := range c.When.Keys {
			parts = append(parts, "`"+w+": "+value.Text(c.When.Vals[i])+"`")
		}
		L = append(L, "**`"+c.Field+"` is required only when "+strings.Join(parts, ", ")+".**", "")
	}

	if t.Directory != "" {
		L = append(L, whereItLives(t), "")
	}

	L = append(L, "## What to put in each field", "")
	for _, name := range names {
		f, v := spec(name), vals(spec(name))
		req := "optional"
		if required[name] {
			req = "required"
		}
		L = append(L, "**`"+name+"`** · "+req+" · `"+valueType(f)+"`  ")
		if f.Guidance != "" {
			L = append(L, f.Guidance)
		} else {
			L = append(L, "_No guidance written yet. Add `guidance:` to this field in the schema._")
		}
		if f.Excludes != "" {
			L = append(L, "", "Never repeats `"+f.Excludes+"`. Checked.")
		}
		if sub := nested(f); len(sub) > 0 {
			L = append(L, "")
			for _, k := range sub {
				kreq := "optional"
				if k.Required {
					kreq = "required"
				}
				line := "- `" + k.Name + "` · " + kreq + " · `" + k.Value + "`"
				if k.Guidance != "" {
					line += ": " + k.Guidance
				}
				if k.Reference != "" {
					line += " Any " + refTarget(k.Reference) + ", checked."
				}
				L = append(L, line)
				for _, d := range k.Enum() {
					line := "  - `" + d.Key + "`: " + d.Means
					if d.Not != "" {
						line += " Not for: " + d.Not
					}
					L = append(L, line)
				}
			}
		} else if f.Reference != "" {
			by := "by its filename without `.md`"
			if strings.HasSuffix(f.Reference, "/*") {
				by = "by its directory name"
			}
			L = append(L, "", "Values: any "+refTarget(f.Reference)+", "+
				by+". Checked, so a name that resolves to no page is a fault.")
		} else if len(v) > 0 {
			ticked := make([]string, len(v))
			for i, x := range v {
				ticked[i] = "`" + x + "`"
			}
			end := "."
			if hasMeanings(f.Enum()) {
				end = ", defined below."
			}
			L = append(L, "", "Values: "+strings.Join(ticked, " · ")+end)
		}
		L = append(L, "")
	}

	wrote := false
	for _, name := range names {
		f := spec(name)
		v, enum := vals(f), f.Enum()
		if len(v) == 0 || !hasMeanings(enum) {
			continue
		}
		if !wrote {
			L = append(L, "## What the values mean", "")
			wrote = true
		}
		L = append(L, "### `"+name+"`", "")
		grouped := false
		if f.Vocabulary != "" {
			L = append(L, "Shared vocabulary from [../vocabularies/"+f.Vocabulary+".yaml](../vocabularies/"+f.Vocabulary+".yaml).", "")
			for _, d := range enum {
				grouped = grouped || d.Group != ""
			}
		}
		nots := false
		for _, d := range enum {
			nots = nots || d.Not != ""
		}
		head, rule := "| value |", "|---|"
		if grouped {
			head, rule = head+" group |", rule+"---|"
		}
		head, rule = head+" means |", rule+"---|"
		if nots {
			head, rule = head+" not for |", rule+"---|"
		}
		L = append(L, head, rule)
		for _, x := range v {
			d := lookup(enum, x)
			row := "| `" + x + "` |"
			if grouped {
				row += " " + d.Group + " |"
			}
			row += " " + cell(d.Means) + " |"
			if nots {
				row += " " + cell(d.Not) + " |"
			}
			L = append(L, row)
		}
		L = append(L, "")
	}

	L = append(L, "## Adding a value", "",
		"A field's `definitions` keys **are** its allowed values, so a value cannot exist until "+
			"someone writes down what it means. Prefer an existing one, because a near-synonym "+
			"fragments every filter. If nothing fits, add it to the right file under "+readmeLink(s)+
			" with its one line, in the same change that uses it. `"+s.Command("check")+"` fails otherwise.", "")
	return strings.Join(L, "\n")
}

func optMark(f *schema.Field) string {
	if f.Required {
		return ""
	}
	return "   # optional"
}

func hasMeanings(enum []schema.Def) bool {
	for _, d := range enum {
		if d.Means != "" {
			return true
		}
	}
	return false
}

func lookup(enum []schema.Def, key string) schema.Def {
	for _, d := range enum {
		if d.Key == key {
			return d
		}
	}
	return schema.Def{}
}

// cell keeps a definition inside its table cell.
func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

func joinAnd(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// refTarget names what a `reference:` resolves against, linked: the pages anywhere
// under a directory, or for `<dir>/*`, the directories directly under it that hold
// an index.md.
func refTarget(ref string) string {
	if dir, ok := strings.CutSuffix(ref, "/*"); ok {
		return "directory under [`" + dir + "/`](../../" + dir + "/) that holds an `index.md`"
	}
	return "page under [`" + ref + "/`](../../" + ref + "/)"
}

// whereItLives says which folder a doctype's pages sit in, from its `directory:`.
func whereItLives(t *schema.Type) string {
	line := "The page lives at `" + t.Directory + "/<slug>.md`"
	if schema.Placeholder.MatchString(t.Directory) {
		line += ", filled from its own frontmatter"
	}
	if prefix := schema.DirectoryPrefix(t.Directory); prefix != "" {
		line += ", and its slug is unique under [`" + prefix + "/`](../../" + prefix + "/)."
	} else {
		line += ", and its slug is unique under [the taxonomy's parent](../../)."
	}
	for _, part := range strings.Split(t.Directory, "/") {
		m := schema.Placeholder.FindStringSubmatch(part)
		if m == nil || m[0] != part || slices.Contains(t.Required, m[1]) {
			continue
		}
		if rest := strings.Trim(strings.Replace("/"+t.Directory+"/", "/"+part+"/", "/", 1), "/"); rest != "" {
			line += " A page with no `" + m[1] + "` sits directly in `" + rest + "/`."
		}
	}
	return line + " A page anywhere else fails the check."
}
