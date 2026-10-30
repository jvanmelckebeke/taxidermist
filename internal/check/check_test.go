package check

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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
	var out []string
	for _, f := range checked(t, rel, body, edit...) {
		out = append(out, f.Kind+" "+f.Field)
	}
	sort.Strings(out)
	return out
}

// checked checks one file written into a copy of example/ and returns its faults.
func checked(t *testing.T, rel, body string, edit ...func(dir string)) []Fault {
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
	return New(s).Run(files).Faults
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

func TestExcludesRejectsARepeatOfTheNamedField(t *testing.T) {
	excl := func(dir string) {
		p := filepath.Join(dir, "taxonomy", "traits.yaml")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, []byte(strings.Replace(string(b), "      value: list\n      guidance: Free-text", "      value: list\n      excludes: topic\n      guidance: Free-text", 1)), 0o644)
	}
	expect(t, faults(t, "notes/x.md", note("tags: [infra, hiring]\n"), excl), "value tags")
	expect(t, faults(t, "notes/x.md", note("tags: [infra, garden]\n"), excl))
	expect(t, faults(t, "meetings/x.md", "---\ntype: meeting\ntitle: m\ndate: 2026-07-02\ntags: [hiring]\n---\n", excl))
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
	expect(t, faults(t, "notes/x.md", note("people: [alice, carol]\n")), "value people[1]")
}

func TestListItemsAreCheckedAsMappings(t *testing.T) {
	m := "---\ntype: meeting\ntitle: m\ndate: 2026-07-02\nactions:\n%s---\n"
	expect(t, faults(t, "meetings/x.md", strings.Replace(m, "%s", "- what: a\n  owner: alice\n  done: true\n", 1)))
	expect(t, faults(t, "meetings/x.md", strings.Replace(m, "%s", "- just a string\n", 1)), "shape actions[0]")
	expect(t, faults(t, "meetings/x.md", strings.Replace(m, "%s", "- what: a\n  owner: carol\n  due: 2026-08-01\n", 1)),
		"shape actions[0].due", "value actions[0].owner")
	expect(t, faults(t, "meetings/x.md", strings.Replace(m, "%s", "- owner: alice\n", 1)), "missing actions[0].what")
}

// withReviewers gives a note's `review:` a list against a vocabulary, a list of
// references, and an object nested inside it.
func withReviewers(dir string) {
	p := filepath.Join(dir, "taxonomy", "types", "note.yaml")
	b, _ := os.ReadFile(p)
	b = []byte(strings.Replace(string(b), "        guidance: When to look again.\n", `        guidance: When to look again.
      topics:
        value: list
        vocabulary: topic
        guidance: What the review covered.
      by:
        value: list
        reference: people
        guidance: Who reviewed.
      sign_off:
        value: object
        guidance: Who signed off.
        schema:
          who:
            value: str
            reference: people
            required: true
            guidance: The person.
          status:
            value: str
            guidance: How it ended.
            definitions:
              agreed: Signed off.
`, 1))
	os.WriteFile(p, b, 0o644)
}

func TestNestedKeysGetTheTopLevelChecks(t *testing.T) {
	r := func(review string) string { return note("review:\n  open: false\n" + review) }
	expect(t, faults(t, "notes/x.md", r("  topics: [hiring, garden]\n  by: [alice]\n  sign_off: {who: alice, status: agreed}\n"), withReviewers))
	expect(t, faults(t, "notes/x.md", r("  topics: [hiring, cooking]\n"), withReviewers), "value review.topics[1]")
	expect(t, faults(t, "notes/x.md", r("  topics: hiring\n"), withReviewers), "type review.topics")
	expect(t, faults(t, "notes/x.md", r("  by: [alice, carol]\n"), withReviewers), "value review.by[1]")
	expect(t, faults(t, "notes/x.md", r("  sign_off: {who: carol, status: maybe, at: 2026-08-01}\n"), withReviewers),
		"value review.sign_off.who", "value review.sign_off.status", "shape review.sign_off.at")
	expect(t, faults(t, "notes/x.md", r("  sign_off: {status: agreed}\n"), withReviewers), "missing review.sign_off.who")
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
		if got := schema.Fnmatch(c.name, c.pat); got != c.want {
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

// A recipe lives in notes/recipes/<kind>/; `course: main` makes the kind required.
const recipeType = `traits: [core]
required: [type, title]
optional: [tags, kind, course]
required_when:
  kind:
    course: main
directory: notes/recipes/{kind}
fields:
  kind: {value: str}
  course: {value: str}
`

func withRecipes(dir string) {
	put(dir, "taxonomy/types/recipe.yaml", recipeType)
}

func recipe(extra string) string { return "---\ntype: recipe\ntitle: r\n" + extra + "---\n" }

func TestAPageLivesInTheDirectoryItsTypeNames(t *testing.T) {
	expect(t, faults(t, "notes/recipes/soup/x.md", recipe("kind: soup\n"), withRecipes))
	expect(t, faults(t, "notes/recipes/bread/x.md", recipe("kind: soup\n"), withRecipes), "value kind")
	expect(t, faults(t, "notes/recipes/soup/deeper/x.md", recipe("kind: soup\n"), withRecipes), "value kind")
	expect(t, faults(t, "notes/x.md", recipe("kind: soup\n"), withRecipes), "value kind")

	f := checked(t, "notes/recipes/bread/x.md", recipe("kind: soup\n"), withRecipes)
	if len(f) != 1 || !regexp.MustCompile(`^the page is in \S*notes/recipes/bread/ but its frontmatter puts it in \S*notes/recipes/soup/$`).MatchString(f[0].Why) {
		t.Errorf("faults = %+v", f)
	}
}

func TestAnOptionalAbsentPlaceholderDropsOutOfThePath(t *testing.T) {
	expect(t, faults(t, "notes/recipes/x.md", recipe(""), withRecipes))
	expect(t, faults(t, "notes/recipes/soup/x.md", recipe(""), withRecipes), "value directory")
}

func TestARequiredAbsentPlaceholderIsOnlyMissing(t *testing.T) {
	expect(t, faults(t, "notes/recipes/x.md", recipe("course: main\n"), withRecipes), "missing kind")
	expect(t, faults(t, "notes/recipes/soup/x.md", recipe("course: main\nkind: soup\n"), withRecipes))
}

func TestADirectoryWithNoPlaceholder(t *testing.T) {
	fixed := func(dir string) {
		put(dir, "taxonomy/types/recipe.yaml", strings.Replace(recipeType, "/{kind}", "", 1))
	}
	expect(t, faults(t, "notes/recipes/x.md", recipe("kind: soup\n"), fixed))
	expect(t, faults(t, "notes/recipes/soup/x.md", recipe("kind: soup\n"), fixed), "value directory")
}

func TestASlugIsUniqueUnderTheFixedPrefix(t *testing.T) {
	twin := func(rel string) func(string) {
		return func(dir string) { put(dir, rel, recipe("kind: bread\n")) }
	}
	expect(t, faults(t, "notes/recipes/soup/x.md", recipe("kind: soup\n"), withRecipes, twin("notes/recipes/bread/x.md")),
		"value <slug>")
	expect(t, faults(t, "notes/recipes/soup/x.md", recipe("kind: soup\n"), withRecipes, twin("notes/recipes/bread/y.md")))
	// Outside the prefix, a page with the same stem is no twin.
	expect(t, faults(t, "notes/recipes/soup/x.md", recipe("kind: soup\n"), withRecipes, twin("notes/x.md")))
	// Every kind folder has its own index.md.
	expect(t, faults(t, "notes/recipes/soup/index.md", recipe("kind: soup\n"), withRecipes, twin("notes/recipes/bread/index.md")))

	f := checked(t, "notes/recipes/soup/x.md", recipe("kind: soup\n"), withRecipes, twin("notes/recipes/bread/x.md"))
	if len(f) != 1 || f[0].Value != "x" || !regexp.MustCompile(`^also used by \S*notes/recipes/bread/x\.md; a slug is unique across \S*notes/recipes/$`).MatchString(f[0].Why) {
		t.Errorf("faults = %+v", f)
	}
}

func TestAReferenceReachesPagesInSubfolders(t *testing.T) {
	carol := func(dir string) { put(dir, "people/team/carol.md", "---\ntype: person\ntitle: Carol\n---\n") }
	expect(t, faults(t, "notes/x.md", note("people: [alice, carol]\n"), carol))
	expect(t, faults(t, "notes/x.md", note("people: [team]\n"), carol), "value people[0]")
}

// withProject gives a note a `project:` naming a folder under projects/.
func withProject(dir string) {
	p := filepath.Join(dir, "taxonomy", "types", "note.yaml")
	b, _ := os.ReadFile(p)
	b = []byte(strings.Replace(string(b), "optional:\n", "optional:\n- project\n", 1))
	b = []byte(strings.Replace(string(b), "fields:\n", "fields:\n  project:\n    value: str\n    reference: projects/*\n", 1))
	put(dir, "taxonomy/types/note.yaml", string(b))
	put(dir, "projects/infra/index.md", "# infra\n")
	put(dir, "projects/garden/plan.md", "# no index\n")
}

func TestADirectoryReferenceNamesAFolderWithAnIndex(t *testing.T) {
	expect(t, faults(t, "notes/x.md", note("project: infra\n"), withProject))
	// A page inside the folder, a page directly under projects/, and a folder with no index.md.
	expect(t, faults(t, "notes/x.md", note("project: build-cache\n"), withProject), "value project")
	expect(t, faults(t, "notes/x.md", note("project: hiring\n"), withProject), "value project")
	expect(t, faults(t, "notes/x.md", note("project: garden\n"), withProject), "value project")

	f := checked(t, "notes/x.md", note("project: garden\n"), withProject)
	if len(f) != 1 || !regexp.MustCompile(`^no page at \S*projects/garden/index\.md\. \S*projects/ has 1 directories with an index\.md$`).MatchString(f[0].Why) {
		t.Errorf("faults = %+v", f)
	}
}

// put writes a file for an edit, which has no *testing.T to fail.
func put(dir, rel, body string) {
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		panic(err)
	}
}

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
