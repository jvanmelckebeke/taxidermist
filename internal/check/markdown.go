package check

import (
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/frontmatter"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

// markdownFile checks a page: the folder it sits in, then its frontmatter against
// the doctype its `type:` names.
func (c *Checker) markdownFile(path string, raw []byte, r *Result, add adder) {
	segs := c.segments(path)
	c.segmentFaults(path, segs, r)
	fm, err := frontmatter.Bytes(raw)
	if err == nil && fm != nil {
		c.mismatches(path, segs, fm, r)
	}
	if err != nil {
		add("parse", "<frontmatter>", nil, "the block opens and does not parse, so the file is invisible: "+firstLine(err.Error()))
		return
	}
	if fm == nil || len(fm.Keys) == 0 {
		return
	}
	t := c.pageType(path, fm, r, add)
	if t == nil {
		return
	}
	required := c.fieldSet("", fm, t, c.exemptions(path), add)
	if t.Directory != "" {
		c.directory(path, fm, t.Directory, required, add)
	}
}

// pageType is the doctype a page's `type:` names. It is nil, with the reason
// recorded as a fault or an ungoverned tally, when there is no such markdown doctype.
func (c *Checker) pageType(path string, fm *value.Mapping, r *Result, add adder) *schema.Type {
	dt, _ := fm.Get("type")
	if dt == nil {
		add("missing", "type", nil, "every governed document declares its type")
		return nil
	}
	name, isStr := dt.(string)
	t := c.S.Types[name]
	if t == nil || !isStr {
		if c.S.Ungoverned == "report" {
			key := value.Text(dt)
			r.Ungoverned[key] = append(r.Ungoverned[key], path)
			return nil
		}
		add("value", "type", dt, "no types/ file declares this doctype. Known: "+strings.Join(c.S.PageDoctypes(), " · "))
		return nil
	}
	if t.IsYAML() {
		add("value", "type", dt, "`"+t.Doctype+"` is the doctype of "+t.Files+", not of a markdown page. Known: "+
			strings.Join(c.S.PageDoctypes(), " · "))
		return nil
	}
	return t
}
