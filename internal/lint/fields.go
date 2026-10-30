package lint

import (
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

// fieldLists checks each doctype's required and optional lists: no field in both,
// and every field it defines in one of them.
func fieldLists(s *schema.Schema, add adder) {
	doctypes := s.Doctypes()
	for _, dt := range doctypes {
		t := s.Types[dt]
		req := map[string]bool{}
		for _, n := range t.Required {
			req[n] = true
		}
		var both []string
		for _, n := range t.Optional {
			if req[n] {
				both = append(both, n)
			}
		}
		if len(both) > 0 {
			add("required-optional-disjoint", "`%s`: %s in both required and optional", dt, strings.Join(both, ", "))
		}
		declared := t.Declared()
		for _, f := range t.Fields.Vals {
			if !declared[f.Name] {
				add("own-fields-allowed", "`%s` defines `%s` but never lists it in required or optional", dt, f.Name)
			}
		}
	}
}

// excludesDeclared: a top-level field's excludes names a field every doctype carrying
// it declares.
func excludesDeclared(s *schema.Schema, add adder) {
	doctypes := s.Doctypes()
	for _, dt := range doctypes {
		t := s.Types[dt]
		declared := t.Declared()
		for _, n := range append(append([]string(nil), t.Required...), t.Optional...) {
			f := t.Resolved[n]
			if f != nil && f.Excludes != "" && !declared[f.Excludes] {
				add("excludes-declared", "`%s`: `%s` excludes `%s`, which `%s` does not declare", dt, n, f.Excludes, dt)
			}
		}
	}
}

// fieldSpecs checks every field spec, nested keys included: excludes only on a
// top-level field, and guidance everywhere.
func fieldSpecs(s *schema.Schema, add adder) {
	eachField(s, func(where string, f *schema.Field) {
		for _, k := range append(append([]*schema.Field(nil), f.Schema.Vals...), f.Items.Vals...) {
			if k.Excludes != "" {
				add("excludes-declared", "%s.%s has excludes, which only a top-level field checks", where, k.Name)
			}
		}
		if f.Guidance == "" {
			add("guidance", "%s has no guidance, so its format page says the schema has no opinion", where)
		}
	})
}

// eachField visits every field spec in the schema, nested keys included.
func eachField(s *schema.Schema, visit func(where string, f *schema.Field)) {
	var walk func(where string, f *schema.Field)
	walk = func(where string, f *schema.Field) {
		visit(where, f)
		for _, k := range f.Schema.Vals {
			walk(where+"."+k.Name, k)
		}
		for _, k := range f.Items.Vals {
			walk(where+"[]."+k.Name, k)
		}
		if f.Values != nil {
			walk(where+".<values>", f.Values)
		}
	}
	for _, tr := range s.Traits {
		for _, f := range tr.Fields {
			walk("traits.yaml "+tr.Name+"."+f.Name, f)
		}
	}
	for _, f := range s.Singletons {
		walk("singletons.yaml "+f.Name, f)
	}
	for _, dt := range s.Doctypes() {
		t := s.Types[dt]
		for _, f := range t.Fields.Vals {
			walk("types/"+t.Slug+".yaml "+f.Name, f)
		}
	}
}
