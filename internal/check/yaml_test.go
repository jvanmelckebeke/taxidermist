package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/testutil"
)

const goodTeams = `platform:
  purpose: Keeps the servers running.
  lead: bob
`

func TestAYAMLFileTakesTheDoctypeItsPathMatches(t *testing.T) {
	expect(t, faults(t, "config/teams.yaml", goodTeams))
	expect(t, faults(t, "config/other.yml", "a: 1\n"), "value <file>")
}

func TestAMapEntryFaultNamesThePathDownToTheKey(t *testing.T) {
	got := faults(t, "config/teams.yaml", goodTeams+"bus/gate:\n  purpose: p\n  lead: nobody\n  archvied: true\n  topics: [cooking]\n")
	expect(t, got, "value bus/gate", "value bus/gate.lead", "unknown bus/gate.archvied", "value bus/gate.topics[0]")
	expect(t, faults(t, "config/teams.yaml", "solo:\n"), "missing solo.purpose", "missing solo.lead")
	expect(t, faults(t, "config/teams.yaml", "solo: lonely\n"), "type solo")
}

func TestAListItemFaultNamesItsIndex(t *testing.T) {
	got := faults(t, "config/rota.yaml", "- {week: 2026-W27, team: platform, person: bob}\n- {week: 27, team: ghosts, person: bob}\n- nope\n")
	expect(t, got, "type [1].week", "value [1].team", "type [2]")
}

func TestTheTopLevelMustBeTheShapeTheDoctypeSays(t *testing.T) {
	expect(t, faults(t, "config/rota.yaml", "week: 2026-W27\n"), "type <file>")
	expect(t, faults(t, "config/teams.yaml", "- platform\n", noTeamVocabulary), "type <file>")
	expect(t, faults(t, "config/site.yaml", "just text\n"), "type <file>")
	expect(t, faults(t, "config/site.yaml", ""), "missing name", "missing timezone")
	expect(t, faults(t, "config/site.yaml", "a: [\n"), "parse <file>")
}

func TestAPatternChecksEveryScalar(t *testing.T) {
	expect(t, faults(t, "config/site.yaml", "name: n\ntimezone: Europe/Brussels\n"))
	expect(t, faults(t, "config/site.yaml", "name: n\ntimezone: tomorrow\n"), "value timezone")
	expect(t, faults(t, "notes/x.md", note(""), func(dir string) {
		p := filepath.Join(dir, "taxonomy", "singletons.yaml")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, []byte(strings.Replace(string(b), "    vocabulary: topic\n", "    vocabulary: topic\n    pattern: '^h'\n", 1)), 0o644)
	}))
	expect(t, faults(t, "notes/x.md", strings.Replace(goodNote, "topic: hiring", "topic: infra", 1), func(dir string) {
		p := filepath.Join(dir, "taxonomy", "singletons.yaml")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, []byte(strings.Replace(string(b), "    vocabulary: topic\n", "    vocabulary: topic\n    pattern: '^h'\n", 1)), 0o644)
	}), "value topic")
}

func TestAMapFieldChecksItsKeysAndValues(t *testing.T) {
	expect(t, faults(t, "config/site.yaml", "name: n\ntimezone: Europe/Brussels\nlabels:\n  wip: w\n  WIP: 3\n"),
		"value labels.WIP", "type labels.WIP")
	expect(t, faults(t, "config/site.yaml", "name: n\ntimezone: Europe/Brussels\nlabels: [wip]\n"), "type labels")
}

func TestAMapFieldWithASchemaChecksEachValuesKeys(t *testing.T) {
	edit := func(dir string) {
		testutil.Write(t, dir, "taxonomy/types/site.yaml", "files: config/site.yaml\nrequired: [topics]\nfields:\n"+
			"  topics:\n    value: map\n    schema:\n      meaning: {value: str, required: true, guidance: g}\n    guidance: g\n")
	}
	expect(t, faults(t, "config/site.yaml", "topics:\n  a: {meaning: m}\n", edit))
	expect(t, faults(t, "config/site.yaml", "topics:\n  a: {meening: m}\n  b: text\n", edit),
		"missing topics.a.meaning", "shape topics.a.meening", "shape topics.b")
}

func TestAReferenceToAYAMLFileTakesItsTopLevelKeys(t *testing.T) {
	got := checked(t, "config/rota.yaml", "- {week: 2026-W27, team: ghosts, person: bob}\n")
	if len(got) != 1 || !strings.Contains(got[0].Why, "config/teams.yaml, which has 2 keys") {
		t.Errorf("faults = %+v", got)
	}
}

func TestTwoDoctypesMatchingOneFileIsAFault(t *testing.T) {
	expect(t, faults(t, "config/site.yaml", "name: n\ntimezone: Europe/Brussels\n", func(dir string) {
		testutil.Write(t, dir, "taxonomy/types/everything.yaml", "files: config/*.yaml\n")
	}), "value <file>")
}

func TestYAMLIsLeftAloneUntilADoctypeHasFiles(t *testing.T) {
	expect(t, faults(t, "config/stray.yaml", "a: 1\n", func(dir string) {
		for _, f := range []string{"site", "team", "rota-week"} {
			os.Remove(filepath.Join(dir, "taxonomy", "types", f+".yaml"))
		}
		os.Remove(filepath.Join(dir, "taxonomy", "vocabularies", "team.yaml"))
	}))
}

// noTeamVocabulary drops the vocabulary read from config/teams.yaml, so a test can
// break that file's shape without breaking the schema load.
func noTeamVocabulary(dir string) {
	os.Remove(filepath.Join(dir, "taxonomy", "vocabularies", "team.yaml"))
	p := filepath.Join(dir, "taxonomy", "types", "site.yaml")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), "    vocabulary: team\n", "", 1)), 0o644)
}

func TestTheTaxonomysOwnYAMLIsNotChecked(t *testing.T) {
	dir := testutil.CopyExample(t)
	scope := filepath.Join(dir, "taxonomy", "scope.yaml")
	b, _ := os.ReadFile(scope)
	os.WriteFile(scope, []byte(strings.Replace(string(b), "- config\n", "- config\n- taxonomy\n", 1)), 0o644)
	s, err := schema.Load(filepath.Join(dir, "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	files, _ := Files(s, nil)
	if r := New(s).Run(files); len(r.Faults) > 0 {
		t.Errorf("faults = %+v", r.Faults)
	}
}

func TestUngovernedYAMLIsTalliedInReportMode(t *testing.T) {
	dir := testutil.CopyExample(t)
	scope := filepath.Join(dir, "taxonomy", "scope.yaml")
	b, _ := os.ReadFile(scope)
	os.WriteFile(scope, []byte(strings.Replace(string(b), "ungoverned: error", "ungoverned: report", 1)), 0o644)
	p := testutil.Write(t, dir, "config/stray.yaml", "a: 1\n")
	s, err := schema.Load(filepath.Join(dir, "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	r := New(s).Run([]string{p})
	if len(r.Faults) != 0 || len(r.Ungoverned[NoFilesMatch]) != 1 {
		t.Errorf("faults %+v, ungoverned %v", r.Faults, r.Ungoverned)
	}
}

func TestAPageCannotTakeAYAMLDoctype(t *testing.T) {
	got := checked(t, "notes/x.md", "---\ntype: team\n---\n")
	if len(got) != 1 || got[0].Field != "type" || !strings.Contains(got[0].Why, "doctype of config/teams.yaml") {
		t.Errorf("faults = %+v", got)
	}
}
