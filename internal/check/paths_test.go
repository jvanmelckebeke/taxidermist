package check

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/testutil"
)

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
	s, err := schema.Load(filepath.Join(dir, ".taxidermist"))
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
		scope := filepath.Join(dir, ".taxidermist.yaml")
		b, _ := os.ReadFile(scope)
		os.WriteFile(scope, []byte(strings.Replace(string(b), "field: topic", "field: tags", 1)), 0o644)
		p := testutil.Write(t, dir, "projects/infra/x.md", body)
		s, err := schema.Load(filepath.Join(dir, ".taxidermist"))
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
	s, err := schema.Load(filepath.Join(testutil.Example(), ".taxidermist"))
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
	put(dir, ".taxidermist/types/recipe.yaml", recipeType)
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
		put(dir, ".taxidermist/types/recipe.yaml", strings.Replace(recipeType, "/{kind}", "", 1))
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

// A convention keeps one file name at several depths:
// conventions/prompting.md beside conventions/<model>/<kind>/prompting.md.
func conventionTree(scope string) func(string) {
	return func(dir string) {
		put(dir, ".taxidermist/types/convention.yaml", "traits: [core]\nrequired: [type, title]\noptional: [tags, model, kind]\n"+
			"directory: notes/conventions/{model}/{kind}\n"+scope+"fields:\n  model: {value: str}\n  kind: {value: str}\n")
		put(dir, "notes/conventions/a/x/prompting.md", "---\ntype: convention\ntitle: t\nmodel: a\nkind: x\n---\n")
		put(dir, "notes/conventions/b/y/prompting.md", "---\ntype: convention\ntitle: t\nmodel: b\nkind: y\n---\n")
	}
}

func convention(extra string) string { return "---\ntype: convention\ntitle: t\n" + extra + "---\n" }

func TestSlugScopePrefixFaultsAShallowTwin(t *testing.T) {
	for _, scope := range []string{"", "slug_scope: prefix\n"} {
		expect(t, faults(t, "notes/conventions/prompting.md", convention(""), conventionTree(scope)), "value <slug>")
		expect(t, faults(t, "notes/conventions/a/x/prompting.md", convention("model: a\nkind: x\n"), conventionTree(scope)), "value <slug>")
	}
}

func TestSlugScopeDirectoryAllowsOneNameAtSeveralDepths(t *testing.T) {
	tree := conventionTree("slug_scope: directory\n")
	expect(t, faults(t, "notes/conventions/prompting.md", convention(""), tree))
	expect(t, faults(t, "notes/conventions/a/x/prompting.md", convention("model: a\nkind: x\n"), tree))
	expect(t, faults(t, "notes/conventions/b/y/prompting.md", convention("model: b\nkind: y\n"), tree))
	// A misplaced page is faulted, and it collides in the folder it should be in.
	expect(t, faults(t, "notes/conventions/b/prompting.md", convention("model: a\nkind: x\n"), tree), "value <slug>", "value model, kind")
}

func TestAReferenceReachesPagesInSubfolders(t *testing.T) {
	carol := func(dir string) { put(dir, "people/team/carol.md", "---\ntype: person\ntitle: Carol\n---\n") }
	expect(t, faults(t, "notes/x.md", note("people: [alice, carol]\n"), carol))
	expect(t, faults(t, "notes/x.md", note("people: [team]\n"), carol), "value people[0]")
}

// withProject gives a note a `project:` naming a folder under projects/.
func withProject(dir string) {
	p := filepath.Join(dir, ".taxidermist", "types", "note.yaml")
	b, _ := os.ReadFile(p)
	b = []byte(strings.Replace(string(b), "optional:\n", "optional:\n- project\n", 1))
	b = []byte(strings.Replace(string(b), "fields:\n", "fields:\n  project:\n    value: str\n    reference: projects/*\n", 1))
	put(dir, ".taxidermist/types/note.yaml", string(b))
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

func TestAFileRootGovernsThatFileAlone(t *testing.T) {
	dir := testutil.CopyExample(t)
	testutil.Write(t, dir, ".taxidermist.yaml", "roots:\n- notes/kept.md\n- config/site.yaml\n- config/teams.yaml\n")
	kept := testutil.Write(t, dir, "notes/kept.md", goodNote)
	beside := testutil.Write(t, dir, "notes/beside.md", "---\ntype: nothing\n---\n")
	site := filepath.Join(dir, "config", "site.yaml")
	other := testutil.Write(t, dir, "config/other.yaml", "a: 1\n")
	s, err := schema.Load(filepath.Join(dir, ".taxidermist"))
	if err != nil {
		t.Fatal(err)
	}
	walked, err := Files(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	teams := filepath.Join(dir, "config", "teams.yaml")
	if want := []string{site, teams, kept}; !reflect.DeepEqual(walked, want) {
		t.Errorf("walked %q, want %q", walked, want)
	}
	named, err := Files(s, []string{kept, beside, site, other})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{kept, site}; !reflect.DeepEqual(named, want) {
		t.Errorf("filtered %q, want %q", named, want)
	}
	if faults := New(s).Run(walked).Faults; len(faults) > 0 {
		t.Errorf("faults %v", faults)
	}
}

// A profile is one aspect of a person, named <person>.<aspect>.md under notes/profiles/.
const profileType = `traits: [core]
required: [type, title, person, aspect]
optional: [tags]
path: notes/profiles/{person}.{aspect}
fields:
  person: {value: str, reference: people, reference_type: person}
  aspect: {value: str}
`

func withProfiles(dir string) {
	put(dir, ".taxidermist/types/profile.yaml", profileType)
	put(dir, "people/team/standup.md", "---\ntype: meeting\ntitle: Standup\n---\n")
}

func profile(person, aspect string) string {
	return "---\ntype: profile\ntitle: p\nperson: " + person + "\naspect: " + aspect + "\n---\n"
}

func TestAPathNamesTheFile(t *testing.T) {
	expect(t, faults(t, "notes/profiles/alice.work.md", profile("alice", "work"), withProfiles))
	expect(t, faults(t, "notes/profiles/alice.home.md", profile("alice", "work"), withProfiles), "value person, aspect")
	expect(t, faults(t, "notes/profiles/bob.work.md", profile("alice", "work"), withProfiles), "value person, aspect")
	expect(t, faults(t, "notes/profiles/alice.md", profile("alice", "work"), withProfiles), "value person, aspect")

	f := checked(t, "notes/profiles/alice.home.md", profile("alice", "work"), withProfiles)
	if len(f) != 1 || f[0].Value != "alice.home" ||
		f[0].Why != "the file is alice.home.md but its frontmatter names it alice.work.md (`path: notes/profiles/{person}.{aspect}`)" {
		t.Errorf("faults = %+v", f)
	}
}

func TestAPathNamesTheDirectoryToo(t *testing.T) {
	expect(t, faults(t, "notes/alice.work.md", profile("alice", "work"), withProfiles), "value directory")
	expect(t, faults(t, "notes/profiles/old/alice.work.md", profile("alice", "work"), withProfiles), "value directory")
}

func TestAPathWithAMissingFieldIsOnlyMissing(t *testing.T) {
	body := "---\ntype: profile\ntitle: p\nperson: alice\n---\n"
	expect(t, faults(t, "notes/profiles/alice.work.md", body, withProfiles), "missing aspect")
}

func TestAPathWithNoFolderChecksTheStemAnywhere(t *testing.T) {
	stemOnly := func(dir string) {
		put(dir, ".taxidermist/types/profile.yaml", strings.Replace(profileType, "notes/profiles/{person}.{aspect}", "'{person}.{aspect}'", 1))
		put(dir, "people/team/standup.md", "---\ntype: meeting\ntitle: Standup\n---\n")
	}
	expect(t, faults(t, "notes/alice.work.md", profile("alice", "work"), stemOnly))
	expect(t, faults(t, "notes/deep/alice.work.md", profile("alice", "work"), stemOnly))
	expect(t, faults(t, "notes/alice.home.md", profile("alice", "work"), stemOnly), "value person, aspect")
}

func TestAReferenceTypeRestrictsThePagesItTakes(t *testing.T) {
	expect(t, faults(t, "notes/profiles/alice.work.md", profile("alice", "work"), withProfiles))
	// standup is a page under people/, but a meeting.
	expect(t, faults(t, "notes/profiles/standup.work.md", profile("standup", "work"), withProfiles), "value person")
	// A name that is no page at all keeps its own fault.
	expect(t, faults(t, "notes/profiles/nobody.work.md", profile("nobody", "work"), withProfiles), "value person")

	f := checked(t, "notes/profiles/standup.work.md", profile("standup", "work"), withProfiles)
	if len(f) != 1 || f[0].Why != "the page is `meeting`; this field takes only `person` pages" {
		t.Errorf("faults = %+v", f)
	}
	f = checked(t, "notes/profiles/nobody.work.md", profile("nobody", "work"), withProfiles)
	if len(f) != 1 || !strings.HasPrefix(f[0].Why, "no page at ") {
		t.Errorf("faults = %+v", f)
	}
}

func TestAReferenceTypeChecksEveryItemOfAList(t *testing.T) {
	typed := func(dir string) {
		p := filepath.Join(dir, ".taxidermist", "singletons.yaml")
		b, _ := os.ReadFile(p)
		put(dir, ".taxidermist/singletons.yaml", strings.Replace(string(b), "reference: people\n", "reference: people\n    reference_type: person\n", 1))
		put(dir, "people/team/standup.md", "---\ntype: meeting\ntitle: Standup\n---\n")
		put(dir, "people/team/untyped.md", "# no frontmatter\n")
	}
	expect(t, faults(t, "notes/x.md", note("people: [alice, bob]\n"), typed))
	expect(t, faults(t, "notes/x.md", note("people: [alice, standup, untyped]\n"), typed), "value people[1]", "value people[2]")

	f := checked(t, "notes/x.md", note("people: [untyped]\n"), typed)
	if len(f) != 1 || f[0].Why != "the page is `no type`; this field takes only `person` pages" {
		t.Errorf("faults = %+v", f)
	}
}

func TestAReferenceTypeOnADirectoryReferenceReadsTheIndex(t *testing.T) {
	typed := func(dir string) {
		withProject(dir)
		p := filepath.Join(dir, ".taxidermist", "types", "note.yaml")
		b, _ := os.ReadFile(p)
		put(dir, ".taxidermist/types/note.yaml", strings.Replace(string(b), "reference: projects/*\n", "reference: projects/*\n    reference_type: Project Plan\n", 1))
		put(dir, "projects/hiring/index.md", "---\ntype: Project Plan\ntitle: Hiring\ntopic: hiring\n---\n")
	}
	expect(t, faults(t, "notes/x.md", note("project: hiring\n"), typed))
	// infra/index.md has no frontmatter, so no type.
	expect(t, faults(t, "notes/x.md", note("project: infra\n"), typed), "value project")
}

func TestASegmentHoldsForYAMLFiles(t *testing.T) {
	expect(t, faults(t, "projects/infra/deploy.yml", "- step: build\n"))
	expect(t, faults(t, "projects/foobar/deploy.yml", "- step: build\n"), "segment projects/*")
	expect(t, faults(t, "projects/garden.yaml", "a: 1\n"))
	expect(t, faults(t, "projects/foobar.yaml", "a: 1\n"), "segment projects/*")
}
