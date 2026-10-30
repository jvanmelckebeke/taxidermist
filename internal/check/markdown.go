package check

import (
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

func (c *Checker) file(path string, raw []byte, r *Result) {
	add := func(kind, field string, v any, why string) {
		r.Faults = append(r.Faults, Fault{Path: path, Kind: kind, Field: field, Value: v, Why: why})
	}
	if schema.IsYAMLPath(path) {
		c.yamlFile(path, raw, r, add)
		return
	}
	segs := c.segments(path)
	for _, sg := range segs {
		if why := c.segmentFault(sg.rule, sg.name); why != "" {
			r.Faults = append(r.Faults, Fault{Path: path, Kind: "segment", Field: sg.rule.Path, Value: sg.name, Why: why, Dir: sg.dir(c.S)})
		}
	}
	fm, err := frontmatter.Bytes(raw)
	if err == nil && fm != nil {
		for _, sg := range segs {
			if sg.rule.Field == "" {
				continue
			}
			v, ok := fm.Get(sg.rule.Field)
			if !ok || hasText(v, sg.name) {
				continue
			}
			r.Warnings = append(r.Warnings, Fault{Path: path, Kind: "mismatch", Field: sg.rule.Field, Value: v,
				Why: "the folder says `" + sg.name + "` (" + sg.rule.Path + ")", Dir: sg.dir(c.S)})
		}
	}
	if err != nil {
		add("parse", "<frontmatter>", nil, "the block opens and does not parse, so the file is invisible: "+firstLine(err.Error()))
		return
	}
	if fm == nil || len(fm.Keys) == 0 {
		return
	}
	dt, _ := fm.Get("type")
	if dt == nil {
		add("missing", "type", nil, "every governed document declares its type")
		return
	}
	name, isStr := dt.(string)
	t := c.S.Types[name]
	if t == nil || !isStr {
		if c.S.Ungoverned == "report" {
			key := value.Text(dt)
			r.Ungoverned[key] = append(r.Ungoverned[key], path)
			return
		}
		add("value", "type", dt, "no types/ file declares this doctype. Known: "+strings.Join(c.markdownDoctypes(), " · "))
		return
	}
	if t.IsYAML() {
		add("value", "type", dt, "`"+t.Doctype+"` is the doctype of "+t.Files+", not of a markdown page. Known: "+
			strings.Join(c.markdownDoctypes(), " · "))
		return
	}
	required := c.fieldSet("", fm, t, c.exemptions(path), add)
	if t.Directory != "" {
		c.directory(path, fm, t.Directory, required, add)
	}
}

// markdownDoctypes are the doctypes a page's `type:` may name.
func (c *Checker) markdownDoctypes() []string {
	var out []string
	for _, dt := range c.S.Doctypes() {
		if !c.S.Types[dt].IsYAML() {
			out = append(out, dt)
		}
	}
	return out
}
