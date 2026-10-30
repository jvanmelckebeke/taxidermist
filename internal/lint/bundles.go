package lint

import (
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

// appliesToKnown: a singleton's applies_to names only doctypes with a types/ file.
func appliesToKnown(s *schema.Schema, add adder) {
	for _, f := range s.Singletons {
		for _, d := range f.AppliesTo {
			if s.Types[d] == nil {
				add("applies-to-known", "singleton `%s` applies to `%s`, which has no types/ file", f.Name, d)
			}
		}
	}
}

// traitBreadth: a trait is composed by at least 3 doctypes.
func traitBreadth(s *schema.Schema, add adder) {
	doctypes := s.Doctypes()
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
}

// singletonNotUniversal: a singleton on (nearly) every frontmatter doctype belongs in
// a trait.
func singletonNotUniversal(s *schema.Schema, add adder) {
	doctypes := s.Doctypes()
	// Traits and singletons bundle frontmatter, so a YAML doctype does not count here.
	pages := 0
	for _, dt := range doctypes {
		if !s.Types[dt].IsYAML() {
			pages++
		}
	}
	for _, f := range s.Singletons {
		n := len(f.AppliesTo)
		if n >= 3 && n >= pages-1 {
			add("singleton-not-universal", "singleton `%s` is on %d of %d frontmatter doctypes; promote it to a trait", f.Name, n, pages)
		}
	}
}

// declaredOnce: a doctype gets each field from exactly one of a trait, a singleton or
// its own file.
func declaredOnce(s *schema.Schema, add adder) {
	doctypes := s.Doctypes()
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
		for _, f := range t.Fields.Vals {
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
}

// traitSingletonOverlap: no field is both a trait field and a singleton.
func traitSingletonOverlap(s *schema.Schema, add adder) {
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
}
