// Package lint checks the schema against itself: the invariants that keep a taxonomy
// readable as it grows. A schema that fails lint still loads and still checks files;
// these are the drifts that make it harder to trust over time.
package lint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

type Problem struct {
	Rule string
	Msg  string
}

// Rules names each check and what it protects.
var Rules = []struct{ ID, Doc string }{
	{"applies-to-known", "a singleton's applies_to names only doctypes that have a types/ file"},
	{"required-optional-disjoint", "no field is in both required and optional; one of them is a leftover"},
	{"own-fields-allowed", "every field a type file defines is in its required or optional list"},
	{"no-values-list", "no field carries a `values:` list; the definitions keys are the values"},
	{"trait-breadth", "a trait is composed by at least 3 doctypes, or it belongs in singletons.yaml"},
	{"singleton-not-universal", "a singleton on (nearly) every doctype belongs in a trait"},
	{"declared-once", "a doctype gets each field from exactly one of a trait, a singleton or its own file"},
	{"trait-singleton-overlap", "no field is both a trait field and a singleton"},
	{"guidance", "every field, and every key of an object or list item, has guidance"},
}

func Run(s *schema.Schema) []Problem {
	var out []Problem
	add := func(rule, format string, a ...any) {
		out = append(out, Problem{rule, fmt.Sprintf(format, a...)})
	}
	doctypes := s.Doctypes()

	for _, f := range s.Singletons {
		for _, d := range f.AppliesTo {
			if s.Types[d] == nil {
				add("applies-to-known", "singleton `%s` applies to `%s`, which has no types/ file", f.Name, d)
			}
		}
	}

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
		for _, f := range t.Fields {
			if !declared[f.Name] {
				add("own-fields-allowed", "`%s` defines `%s` but never lists it in required or optional", dt, f.Name)
			}
		}
	}

	eachField(s, func(where string, f *schema.Field) {
		if f.Raw.Has("values") {
			add("no-values-list", "%s carries a `values:` list; its definitions keys are the values", where)
		}
		if f.Guidance == "" {
			add("guidance", "%s has no guidance, so its format page says the schema has no opinion", where)
		}
	})

	for _, tr := range s.Traits {
		n := 0
		for _, dt := range doctypes {
			if contains(s.Types[dt].Traits, tr.Name) {
				n++
			}
		}
		if n < 3 {
			add("trait-breadth", "trait `%s` is composed by %d doctype(s); a bundle on fewer than 3 is a lookup hop, so move its fields to singletons.yaml", tr.Name, n)
		}
	}

	for _, f := range s.Singletons {
		n := len(f.AppliesTo)
		if n >= 3 && n >= len(doctypes)-1 {
			add("singleton-not-universal", "singleton `%s` is on %d of %d doctypes; promote it to a trait", f.Name, n, len(doctypes))
		}
	}

	for _, dt := range doctypes {
		t := s.Types[dt]
		seen := map[string]int{}
		for _, trn := range t.Traits {
			if tr := s.Trait(trn); tr != nil {
				for _, f := range tr.Fields {
					seen[f.Name]++
				}
			}
		}
		for _, f := range s.Singletons {
			if contains(f.AppliesTo, dt) {
				seen[f.Name]++
			}
		}
		for _, f := range t.Fields {
			seen[f.Name]++
		}
		var dupes []string
		for n, c := range seen {
			if c > 1 {
				dupes = append(dupes, n)
			}
		}
		sort.Strings(dupes)
		if len(dupes) > 0 {
			add("declared-once", "`%s` gets %s from more than one of its traits, singletons and own fields", dt, strings.Join(dupes, ", "))
		}
	}

	inTraits := map[string]bool{}
	for _, tr := range s.Traits {
		for _, f := range tr.Fields {
			inTraits[f.Name] = true
		}
	}
	for _, f := range s.Singletons {
		if inTraits[f.Name] {
			add("trait-singleton-overlap", "`%s` is both a trait field and a singleton", f.Name)
		}
	}
	return out
}

// eachField visits every field spec in the schema, nested keys included.
func eachField(s *schema.Schema, visit func(where string, f *schema.Field)) {
	var walk func(where string, f *schema.Field)
	walk = func(where string, f *schema.Field) {
		visit(where, f)
		for _, k := range f.Schema {
			walk(where+"."+k.Name, k)
		}
		for _, k := range f.Items {
			walk(where+"[]."+k.Name, k)
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
		for _, f := range t.Fields {
			walk("types/"+t.Slug+".yaml "+f.Name, f)
		}
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
