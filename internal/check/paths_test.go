package check

import (
	"os"
	"path/filepath"
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
