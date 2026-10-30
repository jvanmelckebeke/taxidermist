package docs

import (
	"slices"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

// typePage is one doctype's format page, rendered section by section.
type typePage struct {
	s        *schema.Schema
	t        *schema.Type
	names    []string // the doctype's fields: required, then optional
	required map[string]bool
}

func render(s *schema.Schema, t *schema.Type) string {
	p := &typePage{s: s, t: t, names: append(append([]string(nil), t.Required...), t.Optional...), required: map[string]bool{}}
	for _, n := range t.Required {
		p.required[n] = true
	}
	var L []string
	for _, section := range [][]string{p.heading(), p.copyThis(), p.conditions(), p.fieldGuide(), p.meanings(), p.addingAValue()} {
		L = append(L, section...)
	}
	return strings.Join(L, "\n")
}

// spec is a field's spec, or a bare one for a field the doctype lists and nothing
// describes.
func (p *typePage) spec(name string) *schema.Field {
	if f := p.t.Resolved[name]; f != nil {
		return f
	}
	return &schema.Field{Name: name}
}

// heading names the doctype, or the files a YAML doctype governs, and says what the
// file's top level is.
func (p *typePage) heading() []string {
	t := p.t
	L := []string{"# Frontmatter for `type: " + t.Doctype + "`", "", banner(p.s), ""}
	if t.IsYAML() {
		L[0] = "# Format of `" + t.Files + "`"
	}
	if len(t.Traits) > 0 {
		quoted := make([]string, len(t.Traits))
		for i, tr := range t.Traits {
			quoted[i] = "`" + tr + "`"
		}
		L = append(L, "Composes "+joinAnd(quoted)+", the field bundles in [../traits.yaml](../traits.yaml).", "")
	}
	switch t.Shape {
	case "map":
		line := "One entry per top-level key, each with the fields below."
		if t.Key != nil {
			if rule := keyRule(t.Key); rule != "" {
				line += " The key " + rule + "."
			}
			if t.Key.Guidance != "" {
				line += " " + t.Key.Guidance
			}
		}
		L = append(L, line, "")
	case "list":
		L = append(L, "A list, one item per entry, each with the fields below.", "")
	}
	return L
}

// copyThis is the fill-in template: a frontmatter block, or for a YAML doctype a
// whole file, one entry or one item.
func (p *typePage) copyThis() []string {
	t := p.t
	var body []string
	for _, name := range p.names {
		if name == "type" && !t.IsYAML() {
			body = append(body, "type: "+t.Doctype)
			continue
		}
		tail := ""
		if !p.required[name] {
			tail = "   # optional"
		}
		body = append(body, template(name, p.spec(name), tail, "")...)
	}
	L := []string{"## Copy this", "", "```yaml"}
	switch {
	case !t.IsYAML():
		L = append(append(append(L, "---"), body...), "---")
	case t.Shape == "map":
		L = append(L, "<name>:")
		for _, b := range body {
			L = append(L, "  "+b)
		}
	case t.Shape == "list":
		for i, b := range body {
			lead := "  "
			if i == 0 {
				lead = "- "
			}
			L = append(L, lead+b)
		}
	default:
		L = append(L, body...)
	}
	return append(L, "```", "")
}

// conditions are the doctype's `required_when` rules and where its pages live.
func (p *typePage) conditions() []string {
	var L []string
	for _, c := range p.t.RequiredWhen {
		var parts []string
		for i, w := range c.When.Keys {
			parts = append(parts, "`"+w+": "+value.Text(c.When.Vals[i])+"`")
		}
		L = append(L, "**`"+c.Field+"` is required only when "+strings.Join(parts, ", ")+".**", "")
	}
	if p.t.Directory != "" {
		L = append(L, whereItLives(p.t), "")
	}
	return L
}

// fieldGuide says, for every field, what to put there and which values it takes.
func (p *typePage) fieldGuide() []string {
	L := []string{"## What to put in each field", ""}
	for _, name := range p.names {
		f := p.spec(name)
		req := "optional"
		if p.required[name] {
			req = "required"
		}
		L = append(L, "**`"+name+"`** · "+req+" · `"+valueType(f)+"`"+matches(f)+"  ")
		if f.Guidance != "" {
			L = append(L, f.Guidance)
		} else {
			L = append(L, "_No guidance written yet. Add `guidance:` to this field in the schema._")
		}
		if f.Excludes != "" {
			L = append(L, "", "Never repeats `"+f.Excludes+"`. Checked.")
		}
		if f.Key != nil {
			if rule := keyRule(f.Key); rule != "" {
				L = append(L, "", "Each key "+rule+".")
			}
		}
		L = append(append(L, fieldValues(f)...), "")
	}
	return L
}

// fieldValues describes what a field holds: its nested keys, a map's values, the
// pages or keys a reference names, or its allowed values.
func fieldValues(f *schema.Field) []string {
	v := schema.SortedKeys(f.Enum())
	if sub := f.Nested(); len(sub) > 0 || f.Values != nil {
		L := []string{""}
		if f.Values != nil {
			L = append(L, subKey("each value", f.Values, "")...)
		}
		for _, k := range sub {
			kreq := "optional"
			if k.Required {
				kreq = "required"
			}
			L = append(L, subKey("`"+k.Name+"`", k, kreq)...)
		}
		return L
	}
	switch {
	case schema.IsYAMLPath(f.Reference):
		return []string{"", "Values: any " + refTarget(f.Reference) + ". Checked, so a name that is not one is a fault."}
	case f.Reference != "":
		by := "by its filename without `.md`"
		if strings.HasSuffix(f.Reference, "/*") {
			by = "by its directory name"
		}
		return []string{"", "Values: any " + refTarget(f.Reference) + ", " +
			by + ". Checked, so a name that resolves to no page is a fault."}
	case len(v) > 0:
		ticked := make([]string, len(v))
		for i, x := range v {
			ticked[i] = "`" + x + "`"
		}
		end := "."
		if hasMeanings(f.Enum()) {
			end = ", defined below."
		}
		return []string{"", "Values: " + strings.Join(ticked, " · ") + end}
	}
	return nil
}

// meanings is a table per field whose values have definitions.
func (p *typePage) meanings() []string {
	var L []string
	for _, name := range p.names {
		f := p.spec(name)
		if len(f.Enum()) == 0 || !hasMeanings(f.Enum()) {
			continue
		}
		if len(L) == 0 {
			L = append(L, "## What the values mean", "")
		}
		L = append(L, "### `"+name+"`", "")
		if v := p.s.Vocabularies[f.Vocabulary]; v != nil && v.Source != nil {
			L = append(L, "Shared vocabulary "+vocabLink(f.Vocabulary)+", read from [`"+v.Source.From+"`](../../"+v.Source.From+").", "")
		} else if f.Vocabulary != "" {
			L = append(L, "Shared vocabulary from [../vocabularies/"+f.Vocabulary+".yaml](../vocabularies/"+f.Vocabulary+".yaml).", "")
		}
		L = append(L, valuesTable(f.Enum(), f.Vocabulary != "")...)
		L = append(L, "")
	}
	return L
}

func (p *typePage) addingAValue() []string {
	return []string{"## Adding a value", "",
		"A field's `definitions` keys **are** its allowed values, so a value cannot exist until " +
			"someone writes down what it means. Prefer an existing one, because a near-synonym " +
			"fragments every filter. If nothing fits, add it to the right file under " + readmeLink(p.s) +
			" with its one line, in the same change that uses it. `" + p.s.Command("check") + "` fails otherwise.", ""}
}

func matches(f *schema.Field) string {
	if f.Pattern == "" {
		return ""
	}
	return " · matches `" + f.Pattern + "`"
}

// keyRule says what a map's key must be, as a predicate: "matches `^x$`".
func keyRule(k *schema.Field) string {
	var parts []string
	if k.Pattern != "" {
		parts = append(parts, "matches `"+k.Pattern+"`")
	}
	if k.Vocabulary != "" {
		parts = append(parts, "is a value of "+vocabLink(k.Vocabulary))
	} else if v := schema.SortedKeys(k.Enum()); len(v) > 0 {
		parts = append(parts, "is one of `"+strings.Join(v, "` · `")+"`")
	}
	if k.Reference != "" {
		parts = append(parts, "is a "+refTarget(k.Reference))
	}
	return joinAnd(parts)
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
// under a directory, for `<dir>/*` the directories directly under it that hold an
// index.md, or the top-level keys of a YAML file.
func refTarget(ref string) string {
	if schema.IsYAMLPath(ref) {
		return "top-level key of [`" + ref + "`](../../" + ref + ")"
	}
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
