package check

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/testutil"
)

const goodNote = `---
type: note
title: Hiring plan
topic: hiring
date: 2026-07-01
---
`

// faults checks one file written into a copy of example/ and returns "kind field"
// for each fault, sorted.
func faults(t *testing.T, rel, body string, edit ...func(dir string)) []string {
	t.Helper()
	dir := testutil.CopyExample(t)
	for _, e := range edit {
		e(dir)
	}
	p := testutil.Write(t, dir, rel, body)
	s, err := schema.Load(filepath.Join(dir, "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Files(s, []string{p})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range New(s).Run(files).Faults {
		out = append(out, f.Kind+" "+f.Field)
	}
	sort.Strings(out)
	return out
}

func expect(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("faults = %q, want %q", got, want)
	}
}

func note(extra string) string {
	return strings.Replace(goodNote, "date: 2026-07-01\n", "date: 2026-07-01\n"+extra, 1)
}

func TestTheExampleIsClean(t *testing.T) {
	s, err := schema.Load(filepath.Join(testutil.Example(), "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Files(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := New(s).Run(files)
	if len(r.Faults) > 0 || len(files) == 0 {
		t.Errorf("example: %d files, faults %+v", len(files), r.Faults)
	}
}

func TestAConformingEntryIsClean(t *testing.T) {
	expect(t, faults(t, "notes/x.md", goodNote))
}

func TestValueOffTheVocabulary(t *testing.T) {
	expect(t, faults(t, "notes/x.md", strings.Replace(goodNote, "hiring", "recruiting", 1)), "value topic")
}

func TestValueOffTheDefinitions(t *testing.T) {
	expect(t, faults(t, "notes/x.md", note("status: done\n")), "value status")
}

func TestWrongType(t *testing.T) {
	// A date with no day is a string, where every sibling is a date.
	expect(t, faults(t, "notes/x.md", strings.Replace(goodNote, "2026-07-01", "2026-07", 1)), "type date")
}

func TestYesIsAStringNotABool(t *testing.T) {
	expect(t, faults(t, "notes/x.md", strings.Replace(goodNote, "title: Hiring plan", "title: yes", 1)))
	expect(t, faults(t, "notes/x.md", note("status: final\nreview:\n  open: yes\n")), "type review.open")
}

func TestAnImpossibleDateIsNotADate(t *testing.T) {
	expect(t, faults(t, "notes/x.md", strings.Replace(goodNote, "2026-07-01", "2026-02-30", 1)), "type date")
}

func TestMissingRequired(t *testing.T) {
	expect(t, faults(t, "notes/x.md", strings.Replace(goodNote, "topic: hiring\n", "", 1)), "missing topic")
}

func TestUnknownKey(t *testing.T) {
	expect(t, faults(t, "notes/x.md", note("mood: good\n")), "unknown mood")
}

func TestMissingType(t *testing.T) {
	expect(t, faults(t, "notes/x.md", "---\ntitle: x\n---\n"), "missing type")
}

func TestNoFrontmatterIsFine(t *testing.T) {
	expect(t, faults(t, "notes/x.md", "# just prose\n"))
}

func TestUngovernedDoctypeIsAFaultByDefault(t *testing.T) {
	expect(t, faults(t, "notes/x.md", "---\ntype: recipe\n---\n"), "value type")
}

func TestUngovernedDoctypeIsTalliedInReportMode(t *testing.T) {
	dir := testutil.CopyExample(t)
	scope := filepath.Join(dir, "taxonomy", "scope.yaml")
	b, _ := os.ReadFile(scope)
	os.WriteFile(scope, []byte(strings.Replace(string(b), "ungoverned: error", "ungoverned: report", 1)), 0o644)
	p := testutil.Write(t, dir, "notes/x.md", "---\ntype: recipe\n---\n")
	s, err := schema.Load(filepath.Join(dir, "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	r := New(s).Run([]string{p})
	if len(r.Faults) != 0 || len(r.Ungoverned["recipe"]) != 1 || r.Governed() != 0 {
		t.Errorf("faults %+v, ungoverned %v", r.Faults, r.Ungoverned)
	}
}

func TestObjectWrittenAsAString(t *testing.T) {
	expect(t, faults(t, "notes/x.md", note("status: final\nreview: done\n")), "type review")
}

func TestObjectKeyTypo(t *testing.T) {
	expect(t, faults(t, "notes/x.md", note("status: final\nreview:\n  opn: false\n")),
		"missing review.open", "shape review.opn")
}

func TestObjectAcceptsDateAndTimestamp(t *testing.T) {
	expect(t, faults(t, "notes/x.md", note("status: final\nreview:\n  open: true\n  when: 2026-08-01\n")))
	expect(t, faults(t, "notes/x.md", note("status: final\nreview:\n  open: true\n  when: 2026-08-01 10:00:00\n")))
}

func TestRequiredWhenBindsOnlyTheMatchingValue(t *testing.T) {
	expect(t, faults(t, "notes/x.md", note("status: final\n")), "missing review")
	expect(t, faults(t, "notes/x.md", note("status: draft\n")))
	expect(t, faults(t, "notes/x.md", note("status: draft\nreview:\n  open: true\n")))
}

func TestNoUnionFallback(t *testing.T) {
	// `role` is a person's field. A note that lists it would need it declared for notes.
	body := note("role: friend\n")
	expect(t, faults(t, "notes/x.md", body), "unknown role")
	// Allowed by the note's field list but owned by nobody for notes: does not apply.
	expect(t, faults(t, "notes/x.md", body, func(dir string) {
		p := filepath.Join(dir, "taxonomy", "types", "note.yaml")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, []byte(strings.Replace(string(b), "- review\nrequired_when", "- review\n- role\nrequired_when", 1)), 0o644)
	}), "value role")
}

func TestDeclaredExemptionIsHonoured(t *testing.T) {
	body := strings.Replace(goodNote, "topic: hiring\n", "", 1)
	expect(t, faults(t, "notes/archive/x.md", body))
	expect(t, faults(t, "notes/x.md", body), "missing topic")
}

func TestReferenceMustNameAPage(t *testing.T) {
	expect(t, faults(t, "notes/x.md", note("people: [alice, carol]\n")), "value people")
}

func TestListItemsAreCheckedAsMappings(t *testing.T) {
	m := "---\ntype: meeting\ntitle: m\ndate: 2026-07-02\nactions:\n%s---\n"
	expect(t, faults(t, "meetings/x.md", strings.Replace(m, "%s", "- what: a\n  owner: alice\n  done: true\n", 1)))
	expect(t, faults(t, "meetings/x.md", strings.Replace(m, "%s", "- just a string\n", 1)), "shape actions[0]")
	expect(t, faults(t, "meetings/x.md", strings.Replace(m, "%s", "- what: a\n  owner: carol\n  due: 2026-08-01\n", 1)),
		"shape actions[0].due", "value actions[0].owner")
	expect(t, faults(t, "meetings/x.md", strings.Replace(m, "%s", "- owner: alice\n", 1)), "missing actions[0].what")
}

func TestLiteralDoctypeWithASpace(t *testing.T) {
	expect(t, faults(t, "notes/x.md", "---\ntype: Project Plan\ntitle: p\ntopic: infra\n---\n"))
}

func TestFrontmatterThatDoesNotParse(t *testing.T) {
	expect(t, faults(t, "notes/x.md", "---\ntype: note\ndescription: \"a \"quoted\" word\"\n---\n"), "parse <frontmatter>")
	expect(t, faults(t, "notes/x.md", "---\ntype: note\ntitle: a\ntitle: b\n---\n"), "parse <frontmatter>")
	expect(t, faults(t, "notes/x.md", "---\ntype: note\n"), "parse <frontmatter>")
}

func TestUnhashableValuesAreFaultsNotCrashes(t *testing.T) {
	expect(t, faults(t, "notes/x.md", strings.Replace(goodNote, "topic: hiring", "topic: [[a]]", 1)), "type topic")
	expect(t, faults(t, "notes/x.md", "---\ntype: [note]\n---\n"), "value type")
}

func TestFilesOutsideTheRootsAreDropped(t *testing.T) {
	expect(t, faults(t, "elsewhere/x.md", "---\ntype: nonsense\n---\n"))
}

func TestAMissingRootIsASchemaError(t *testing.T) {
	dir := testutil.CopyExample(t)
	os.RemoveAll(filepath.Join(dir, "meetings"))
	s, err := schema.Load(filepath.Join(dir, "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Files(s, nil); err == nil || !strings.Contains(err.Error(), "meetings") {
		t.Errorf("err = %v", err)
	}
}

func TestFnmatch(t *testing.T) {
	cases := []struct {
		name, pat string
		want      bool
	}{
		{"notes/archive/a/b.md", "notes/archive/**", true},
		{"notes/archive/a/b.md", "notes/archive/*", true},
		{"notes/a.md", "notes/?.md", true},
		{"notes/ab.md", "notes/?.md", false},
		{"notes/a.md", "notes/[ab].md", true},
		{"notes/c.md", "notes/[!ab].md", true},
		{"notes/a.md", "notes/[!ab].md", false},
		{"notes/a+b.md", "notes/a+b.md", true},
	}
	for _, c := range cases {
		if got := Fnmatch(c.name, c.pat); got != c.want {
			t.Errorf("Fnmatch(%q, %q) = %v", c.name, c.pat, got)
		}
	}
}

const plan = "---\ntype: Project Plan\ntitle: p\ntopic: %s\n---\n"

func planOn(topic string) string { return strings.Replace(plan, "%s", topic, 1) }

func TestAFolderMustBeAVocabularyValue(t *testing.T) {
	expect(t, faults(t, "projects/infra/x.md", planOn("infra")))
	expect(t, faults(t, "projects/foobar/foobar.md", planOn("infra")), "segment projects/*")
	// Only the first component under the directory is a segment.
	expect(t, faults(t, "projects/infra/foobar/x.md", planOn("infra")))
}

func TestAPageDirectlyUnderTheDirectoryIsNamedByItsStem(t *testing.T) {
	expect(t, faults(t, "projects/garden.md", planOn("garden")))
	expect(t, faults(t, "projects/foobar.md", planOn("infra")), "segment projects/*")
	expect(t, faults(t, "projects/index.md", planOn("infra")))
	expect(t, faults(t, "projects/README.md", planOn("infra")))
}

func TestASegmentHoldsWithoutFrontmatter(t *testing.T) {
	expect(t, faults(t, "projects/foobar/x.md", "# just prose\n"), "segment projects/*")
	expect(t, faults(t, "projects/foobar/x.md", "---\ntype: note\n"), "parse <frontmatter>", "segment projects/*")
}

func TestASegmentPattern(t *testing.T) {
	body := "---\ntype: person\ntitle: Carol\nrole: friend\n---\n"
	expect(t, faults(t, "people/carol.md", body))
	expect(t, faults(t, "people/Carol.md", body), "segment people/*")
}

func run(t *testing.T, rel, body string) *Result {
	t.Helper()
	dir := testutil.CopyExample(t)
	p := testutil.Write(t, dir, rel, body)
	s, err := schema.Load(filepath.Join(dir, "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	return New(s).Run([]string{p})
}

func TestAFieldThatDisagreesWithItsFolderWarns(t *testing.T) {
	r := run(t, "projects/infra/x.md", planOn("hiring"))
	if len(r.Faults) != 0 || len(r.Warnings) != 1 {
		t.Fatalf("faults %+v, warnings %+v", r.Faults, r.Warnings)
	}
	w := r.Warnings[0]
	if w.Kind != "mismatch" || w.Field != "topic" || w.Value != "hiring" || !strings.HasSuffix(w.Dir, filepath.Join("projects", "infra")) {
		t.Errorf("warning %+v", w)
	}
	if r := run(t, "projects/infra/x.md", planOn("infra")); len(r.Warnings) != 0 {
		t.Errorf("agreeing file warned: %+v", r.Warnings)
	}
}

func TestAListFieldAgreesWhenItHoldsTheFolder(t *testing.T) {
	edit := func(body string) *Result {
		t.Helper()
		dir := testutil.CopyExample(t)
		scope := filepath.Join(dir, "taxonomy", "scope.yaml")
		b, _ := os.ReadFile(scope)
		os.WriteFile(scope, []byte(strings.Replace(string(b), "field: topic", "field: tags", 1)), 0o644)
		p := testutil.Write(t, dir, "projects/infra/x.md", body)
		s, err := schema.Load(filepath.Join(dir, "taxonomy"))
		if err != nil {
			t.Fatal(err)
		}
		return New(s).Run([]string{p})
	}
	if r := edit(planOn("infra")); len(r.Warnings) != 0 {
		t.Errorf("no field, no warning: %+v", r.Warnings)
	}
	body := "---\ntype: Project Plan\ntitle: p\ntopic: infra\ntags: [ops, infra]\n---\n"
	if r := edit(body); len(r.Warnings) != 0 {
		t.Errorf("list holding the folder warned: %+v", r.Warnings)
	}
	body = "---\ntype: Project Plan\ntitle: p\ntopic: infra\ntags: [ops]\n---\n"
	if r := edit(body); len(r.Warnings) != 1 {
		t.Errorf("list without the folder: %+v", r.Warnings)
	}
}

func TestFacetsListValuesWithNoFolder(t *testing.T) {
	s, err := schema.Load(filepath.Join(testutil.Example(), "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Files(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	Facets(&b, s, files)
	for _, want := range []string{
		"projects/*  topic: 2 name(s) in use, 2 value(s) with no folder: garden · house",
		"people/*  pattern ^[a-z][a-z-]*$: 2 name(s) in use",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("facets lacks %q:\n%s", want, b.String())
		}
	}
}
