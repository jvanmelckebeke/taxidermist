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

func TestTheTaxonomyDirectoryIsNeverWalked(t *testing.T) {
	dir := testutil.CopyExample(t)
	scope := filepath.Join(dir, "taxonomy", "scope.yaml")
	b, _ := os.ReadFile(scope)
	os.WriteFile(scope, []byte(strings.Replace(string(b), "- config\n", "- config\n- taxonomy\n", 1)), 0o644)
	s, err := schema.Load(filepath.Join(dir, "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	files, _ := Files(s, nil)
	for _, p := range files {
		if strings.Contains(p, string(filepath.Separator)+"taxonomy"+string(filepath.Separator)) {
			t.Errorf("walked %s", p)
		}
	}
	if r := New(s).Run(files); r.Governed() != 10 {
		t.Errorf("governed = %d, want the 10 content files", r.Governed())
	}
}
