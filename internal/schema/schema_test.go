package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/testutil"
)

func loadWith(t *testing.T, rel, body string) error {
	t.Helper()
	dir := testutil.CopyExample(t)
	testutil.Write(t, filepath.Join(dir, "taxonomy"), rel, body)
	_, err := schema.Load(filepath.Join(dir, "taxonomy"))
	return err
}

func TestLoadErrors(t *testing.T) {
	cases := []struct{ name, rel, body, want string }{
		{"unknown trait", "types/x.yaml", "traits: [nope]\nrequired: [type]\n", "unknown trait `nope`"},
		{"trait field not allowed", "types/x.yaml", "traits: [core]\nrequired: [type]\n", "never allows tags, title"},
		{"duplicate doctype", "types/other.yaml", "type: note\n", "both declare `type: note`"},
		{"unknown vocabulary", "types/x.yaml", "required: [type, f]\nfields:\n  f: {value: str, vocabulary: nope}\n", "vocabularies/nope.yaml does not exist"},
		{"definitions and vocabulary", "types/x.yaml", "required: [type, f]\nfields:\n  f: {value: str, vocabulary: topic, definitions: {a: b}}\n", "both `definitions:` and `vocabulary:`"},
		{"bad ungoverned", "scope.yaml", "roots: [notes]\nungoverned: ignore\n", "takes `error` or `report`"},
		{"vocabulary without definitions", "vocabularies/v.yaml", "values: [a]\n", "no `definitions:` mapping"},
		{"unparseable", "traits.yaml", "traits: [\n", "does not parse"},
	}
	for _, c := range cases {
		err := loadWith(t, c.rel, c.body)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestTypeDefaultsToTheFileStem(t *testing.T) {
	s, err := schema.Load(filepath.Join(testutil.Example(), "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Types["note"] == nil || s.Types["Project Plan"] == nil || s.Types["Project Plan"].Slug != "project-plan" {
		t.Errorf("doctypes = %v", s.Doctypes())
	}
}

func TestOwnFieldsWinOverSingletonsOverTraits(t *testing.T) {
	dir := testutil.CopyExample(t)
	tax := filepath.Join(dir, "taxonomy")
	p := filepath.Join(tax, "types", "note.yaml")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(string(b)+"  topic:\n    value: str\n    definitions:\n      only: The note's own list.\n    guidance: g\n"), 0o644)
	s, err := schema.Load(tax)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Types["note"].Resolved["topic"].Enum(s); len(got) != 1 || got[0].Key.String() != "only" {
		t.Errorf("note topic enum = %v", got)
	}
	if got := s.Types["Project Plan"].Resolved["topic"].Enum(s); len(got) != 4 {
		t.Errorf("Project Plan topic enum = %v", got)
	}
}
