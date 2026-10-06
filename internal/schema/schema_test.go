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
		{"unparseable", "traits.yaml", "traits: [\n", "traits.yaml"},
		{"typo in a field spec", "types/x.yaml", "required: [type, f]\nfields:\n  f: {value: str, defintions: {a: b}}\n", "unknown key `defintions`"},
		{"typo in a type file", "types/x.yaml", "requried: [type]\n", "unknown key `requried`"},
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
	if got := s.Types["note"].Resolved["topic"].Enum(); len(got) != 1 || got[0].Key != "only" {
		t.Errorf("note topic enum = %v", got)
	}
	if got := s.Types["Project Plan"].Resolved["topic"].Enum(); len(got) != 4 {
		t.Errorf("Project Plan topic enum = %v", got)
	}
}

func TestSegmentLoadErrors(t *testing.T) {
	scope := "roots: [notes, people, meetings, projects]\nsegments:\n%s"
	cases := []struct{ name, seg, want string }{
		{"missing vocabulary", "- {path: projects/*, vocabulary: nope}\n", "vocabularies/nope.yaml does not exist"},
		{"both", "- {path: projects/*, vocabulary: topic, pattern: x}\n", "exactly one of"},
		{"neither", "- {path: projects/*}\n", "exactly one of"},
		{"bad pattern", "- {path: people/*, pattern: '('}\n", "pattern"},
		{"no trailing star", "- {path: projects, vocabulary: topic}\n", "followed by `/*`"},
		{"star mid-path", "- {path: projects/*/x/*, vocabulary: topic}\n", "no other wildcard"},
		{"typo", "- {path: projects/*, vocabulary: topic, feild: topic}\n", "unknown key `feild`"},
	}
	for _, c := range cases {
		err := loadWith(t, "scope.yaml", strings.Replace(scope, "%s", c.seg, 1))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestADefinitionCanSayWhatAValueIsNotFor(t *testing.T) {
	dir := testutil.CopyExample(t)
	tax := filepath.Join(dir, "taxonomy")
	testutil.Write(t, tax, "vocabularies/v.yaml", "definitions:\n"+
		"  plain: A string.\n"+
		"  full: {means: Yes this., not: Not that.}\n"+
		"  grp:\n    inner: {means: In a group., not: Not outside it.}\n    flat: Also in a group.\n")
	s, err := schema.Load(tax)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]schema.Def{}
	for _, d := range s.Vocabularies["v"] {
		got[d.Key] = d
	}
	want := map[string]schema.Def{
		"plain": {Key: "plain", Means: "A string."},
		"full":  {Key: "full", Means: "Yes this.", Not: "Not that."},
		"inner": {Key: "inner", Means: "In a group.", Not: "Not outside it.", Group: "grp"},
		"flat":  {Key: "flat", Means: "Also in a group.", Group: "grp"},
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %+v, want %+v", k, got[k], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("defs = %+v", got)
	}

	for _, bad := range []string{"definitions:\n  x: {not: only a not}\n", "definitions:\n  x: {means: m, nott: typo}\n"} {
		testutil.Write(t, tax, "vocabularies/v.yaml", bad)
		if _, err := schema.Load(tax); err == nil {
			t.Errorf("loaded %q", bad)
		}
	}
}

func TestFieldDefinitionsTakeTheSameForms(t *testing.T) {
	dir := testutil.CopyExample(t)
	tax := filepath.Join(dir, "taxonomy")
	p := filepath.Join(tax, "types", "note.yaml")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), "draft: Still being written.",
		"draft: {means: Still being written., not: Abandoned; delete those.}", 1)), 0o644)
	s, err := schema.Load(tax)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range s.Types["note"].Resolved["status"].Enum() {
		if d.Key == "draft" && d.Not != "Abandoned; delete those." {
			t.Errorf("draft = %+v", d)
		}
	}
}
