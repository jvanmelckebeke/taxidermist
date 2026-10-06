package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/testutil"
)

func rules(t *testing.T, edit func(tax string)) []string {
	t.Helper()
	dir := testutil.CopyExample(t)
	tax := filepath.Join(dir, "taxonomy")
	if edit != nil {
		edit(tax)
	}
	s, err := schema.Load(tax)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range Run(s) {
		out = append(out, p.Rule)
	}
	return out
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, text...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceIn(t *testing.T, path, old, new string) {
	t.Helper()
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644)
}

func TestTheExamplePasses(t *testing.T) {
	if got := rules(t, nil); len(got) > 0 {
		t.Errorf("example fails %v", got)
	}
}

func TestEachRuleFires(t *testing.T) {
	cases := map[string]func(tax string){
		"applies-to-known": func(tax string) {
			replaceIn(t, filepath.Join(tax, "singletons.yaml"), "    - Project Plan\n", "    - Project Plan\n    - recipe\n")
		},
		"required-optional-disjoint": func(tax string) {
			replaceIn(t, filepath.Join(tax, "types", "person.yaml"), "optional:\n- tags\n", "optional:\n- tags\n- title\n")
		},
		"own-fields-allowed": func(tax string) {
			appendTo(t, filepath.Join(tax, "types", "person.yaml"), "  age:\n    value: int\n    guidance: g\n")
		},
		"trait-breadth": func(tax string) {
			appendTo(t, filepath.Join(tax, "traits.yaml"), "  lonely:\n    mood:\n      value: str\n      guidance: g\n")
		},
		"singleton-not-universal": func(tax string) {
			replaceIn(t, filepath.Join(tax, "singletons.yaml"), "    - note\n    - meeting\n    guidance: When", "    - note\n    - meeting\n    - person\n    guidance: When")
			replaceIn(t, filepath.Join(tax, "types", "person.yaml"), "- role\n", "- role\n- date\n")
		},
		"declared-once": func(tax string) {
			appendTo(t, filepath.Join(tax, "types", "meeting.yaml"), "  date:\n    value: date\n    guidance: g\n")
		},
		"trait-singleton-overlap": func(tax string) {
			appendTo(t, filepath.Join(tax, "singletons.yaml"), "  tags:\n    value: list\n    applies_to: []\n    guidance: g\n")
		},
		"segment-path": func(tax string) {
			replaceIn(t, filepath.Join(tax, "scope.yaml"), "- path: projects/*", "- path: projets/*")
		},
		"segment-field": func(tax string) {
			replaceIn(t, filepath.Join(tax, "scope.yaml"), "field: topic", "field: topik")
		},
		"guidance": func(tax string) {
			replaceIn(t, filepath.Join(tax, "types", "person.yaml"), "    guidance: How you know them.\n", "")
		},
	}
	for rule, edit := range cases {
		got := rules(t, edit)
		found := false
		for _, r := range got {
			found = found || r == rule
		}
		if !found {
			t.Errorf("%s: rules fired %v", rule, got)
		}
	}
	if len(cases) != len(Rules) {
		t.Errorf("%d rules, %d tested", len(Rules), len(cases))
	}
}

func TestASegmentOutsideTheRootsIsFlagged(t *testing.T) {
	got := rules(t, func(tax string) {
		replaceIn(t, filepath.Join(tax, "scope.yaml"), "- meetings\n- projects\n", "- meetings\n")
	})
	if len(got) != 1 || got[0] != "segment-path" {
		t.Errorf("rules fired %v", got)
	}
}
