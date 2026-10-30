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

	L := []string{"# Frontmatter for `type: " + t.Doctype + "`", "", banner(s), ""}
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

	var body []string
	for _, name := range names {
		if name == "type" && !t.IsYAML() {
			body = append(body, "type: "+t.Doctype)
			continue
		}
		tail := ""
		if !required[name] {
			tail = "   # optional"
		}
		body = append(body, template(name, spec(name), tail, "")...)
	}
	L = append(L, "## Copy this", "", "```yaml")
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
	L = append(L, "```", "")

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
		f := spec(name)
		v := enumKeys(f)
		req := "optional"
		if required[name] {
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
		if sub := nested(f); len(sub) > 0 || f.Values != nil {
			L = append(L, "")
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
		} else if schema.IsYAMLPath(f.Reference) {
			L = append(L, "", "Values: any "+refTarget(f.Reference)+". Checked, so a name that is not one is a fault.")
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
		if len(enumKeys(f)) == 0 || !hasMeanings(f.Enum()) {
			continue
		}
		if !wrote {
			L = append(L, "## What the values mean", "")
			wrote = true
		}
		L = append(L, "### `"+name+"`", "")
		if src, ok := s.Sources[f.Vocabulary]; ok {
			L = append(L, "Shared vocabulary "+vocabLink(f.Vocabulary)+", read from [`"+src.From+"`](../../"+src.From+").", "")
		} else if f.Vocabulary != "" {
			L = append(L, "Shared vocabulary from [../vocabularies/"+f.Vocabulary+".yaml](../vocabularies/"+f.Vocabulary+".yaml).", "")
		}
		L = append(L, valuesTable(f.Enum(), f.Vocabulary != "")...)
		L = append(L, "")
	}

	L = append(L, "## Adding a value", "",
		"A field's `definitions` keys **are** its allowed values, so a value cannot exist until "+
			"someone writes down what it means. Prefer an existing one, because a near-synonym "+
			"fragments every filter. If nothing fits, add it to the right file under "+readmeLink(s)+
			" with its one line, in the same change that uses it. `"+s.Command("check")+"` fails otherwise.", "")
	return strings.Join(L, "\n")
}

// template is a field's lines in the "Copy this" block, at the given indent, with
// every nested key, map value and list item below it.
func template(name string, f *schema.Field, tail, in string) []string {
	sub := nested(f)
	switch {
	case f.Value == "map":
		out := []string{in + name + ":" + tail}
		if f.Values != nil {
			return append(out, template("<name>", f.Values, "", in+"  ")...)
		}
		out = append(out, in+"  <name>:")
		for _, k := range sub {
			out = append(out, template(k.Name, k, optMark(k), in+"    ")...)
		}
		return out
	case len(sub) > 0 && f.Value == "object":
		out := []string{in + name + ":" + tail}
		for _, k := range sub {
			out = append(out, template(k.Name, k, optMark(k), in+"  ")...)
		}
		return out
	case len(sub) > 0:
		out := []string{in + name + ":" + tail}
		for i, k := range sub {
			lines := template(k.Name, k, optMark(k), in+"    ")
			if i == 0 {
				lines[0] = in + "  - " + strings.TrimPrefix(lines[0], in+"    ")
			}
			out = append(out, lines...)
		}
		return out
	}
	v := enumKeys(f)
	switch {
	case len(v) > 0 && f.Value == "list":
		return []string{in + name + ": [<" + strings.Join(v, " | ") + ">]" + tail}
	case len(v) > 0:
		return []string{in + name + ": <" + strings.Join(v, " | ") + ">" + tail}
	case f.Value == "list":
		return []string{in + name + ": []" + tail}
	}
	return []string{in + name + ": <" + valueType(f) + ">" + tail}
}

// subKey describes a nested key, or a map's values, as a list item with its allowed
// values below it.
func subKey(name string, k *schema.Field, req string) []string {
	line := "- " + name
	if req != "" {
		line += " · " + req
	}
	line += " · `" + valueType(k) + "`" + matches(k)
	if k.Guidance != "" {
		line += ": " + k.Guidance
	}
	if k.Reference != "" {
		line += " Any " + refTarget(k.Reference) + ", checked."
	}
	out := []string{line}
	for _, d := range k.Enum() {
		line := "  - `" + d.Key + "`: " + d.Means
		if d.Not != "" {
			line += " Not for: " + d.Not
		}
		out = append(out, line)
	}
	return out
}

// enumKeys is a field's allowed values, sorted, once each.
func enumKeys(f *schema.Field) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range f.Enum() {
		if !seen[d.Key] {
			seen[d.Key] = true
			out = append(out, d.Key)
		}
	}
	sort.Strings(out)
	return out
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
	} else if v := enumKeys(k); len(v) > 0 {
		parts = append(parts, "is one of `"+strings.Join(v, "` · `")+"`")
	}
	if k.Reference != "" {
		parts = append(parts, "is a "+refTarget(k.Reference))
	}
	return joinAnd(parts)
}

// vocabLink links a vocabulary's own page from a page in format/.
func vocabLink(name string) string {
	return "[`" + name + "`](vocabularies/" + name + ".md)"
}

// valuesTable is the table of a field's or a vocabulary's values and what each means.
func valuesTable(enum []schema.Def, groups bool) []string {
	grouped, nots := false, false
	for _, d := range enum {
		grouped = grouped || (groups && d.Group != "")
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
	L := []string{head, rule}
	seen := map[string]bool{}
	var keys []string
	for _, d := range enum {
		if !seen[d.Key] {
			seen[d.Key] = true
			keys = append(keys, d.Key)
		}
	}
	sort.Strings(keys)
	for _, x := range keys {
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
	return L
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
