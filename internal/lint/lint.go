// Package lint checks the schema against itself: the invariants that keep a taxonomy
// readable as it grows. A schema that fails lint still loads and still checks files;
// these are the drifts that make it harder to trust over time.
package lint

import (
	"fmt"

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
	{"trait-breadth", "a trait is composed by at least 3 doctypes, or it belongs in singletons.yaml"},
	{"singleton-not-universal", "a singleton on (nearly) every doctype belongs in a trait"},
	{"declared-once", "a doctype gets each field from exactly one of a trait, a singleton or its own file"},
	{"trait-singleton-overlap", "no field is both a trait field and a singleton"},
	{"guidance", "every field, and every key of an object or list item, has guidance"},
	{"segment-path", "a segment rule's directory exists and lies under a governed root, or nothing checks it"},
	{"segment-field", "a segment rule's field is one some doctype declares"},
	{"excludes-declared", "a field's excludes names a field every doctype carrying it declares, and sits on a top-level field"},
	{"files-under-root", "a doctype's files: glob lies under a governed root, or nothing checks the files it names"},
	{"files-one-type", "every YAML file under a governed root matches the files: of at most one doctype"},
}

// adder records one problem under a rule.
type adder func(rule, format string, a ...any)

func Run(s *schema.Schema) []Problem {
	var out []Problem
	add := func(rule, format string, a ...any) {
		out = append(out, Problem{rule, fmt.Sprintf(format, a...)})
	}
	for _, step := range []func(*schema.Schema, adder){
		appliesToKnown,
		fieldLists,
		excludesDeclared,
		fieldSpecs,
		traitBreadth,
		singletonNotUniversal,
		declaredOnce,
		traitSingletonOverlap,
		segmentRules,
		filesUnderRoot,
		filesOneType,
	} {
		step(s, add)
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
