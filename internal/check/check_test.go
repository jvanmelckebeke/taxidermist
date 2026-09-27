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

func TestYes_IsABoolAsPyYAMLReadsIt(t *testing.T) {
	expect(t, faults(t, "notes/x.md", strings.Replace(goodNote, "title: Hiring plan", "title: yes", 1)), "type title")
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
	expect(t, faults(t, "notes/x.md", "---\ntype: note\ndate: 2026-02-30\n---\n"), "parse <frontmatter>")
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
