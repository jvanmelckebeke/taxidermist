package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/testutil"
)

func TestExamplePagesAreCurrent(t *testing.T) {
	s, err := schema.Load(filepath.Join(testutil.Example(), "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	stale, _, total, err := Sync(s, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) > 0 {
		t.Errorf("stale: %v (run taxidermist docs in example/)", stale)
	}
	if total != len(s.Types)+len(s.Vocabularies)+1 {
		t.Errorf("%d pages for %d doctypes and %d vocabularies", total, len(s.Types), len(s.Vocabularies))
	}
}

func TestWriteThenCheckAndPruneStrayPages(t *testing.T) {
	dir := testutil.CopyExample(t)
	tax := filepath.Join(dir, "taxonomy")
	stray := testutil.Write(t, tax, "format/gone.md", "old\n")
	s, err := schema.Load(tax)
	if err != nil {
		t.Fatal(err)
	}
	if stale, _, _, _ := Sync(s, true); len(stale) != 1 || stale[0] != stray {
		t.Errorf("check reported %v", stale)
	}
	if _, _, _, err := Sync(s, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Error("stray page survived a write")
	}
	if stale, _, _, _ := Sync(s, true); len(stale) != 0 {
		t.Errorf("stale after write: %v", stale)
	}
}

func TestPageContent(t *testing.T) {
	s, err := schema.Load(filepath.Join(testutil.Example(), "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	pages, err := Pages(s)
	if err != nil {
		t.Fatal(err)
	}
	for page, wants := range map[string][]string{
		"note": {"type: note", "topic: <garden | hiring | house | infra>", "**`review` is required only when `status: final`.**",
			"| value | group | means | not for |",
			"| `garden` | home | Anything that grows. |  |",
			"| `infra` | work | Servers, deploys, the build. | Hiring for the infra team; that is `hiring`. |",
			"  open: <bool>", "  when: <date>   # optional"},
		"meeting":      {"  - what: <str>", "    done: <bool>   # optional", "[`people/`](../../people/)"},
		"project-plan": {"type: Project Plan"},
		"README": {"- [`Project Plan`](project-plan.md)", "**Frontmatter describes the document.**",
			"- `projects/<name>`: `<name>` is a value of [`topic`](../vocabularies/topic.yaml). A file whose `topic:` names a different value gets a warning.",
			"- `people/<name>`: `<name>` matches `^[a-z][a-z-]*$`."},
	} {
		for _, w := range wants {
			if !strings.Contains(pages[page], w) {
				t.Errorf("%s.md lacks %q", page, w)
			}
		}
	}
	for page, body := range pages {
		if strings.Contains(body, "—") {
			t.Errorf("%s.md has an em dash", page)
		}
	}
}

func TestAListWithAVocabularyRendersAsAList(t *testing.T) {
	dir := testutil.CopyExample(t)
	tax := filepath.Join(dir, "taxonomy")
	p := filepath.Join(tax, "traits.yaml")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), "      value: list\n      guidance: Free-text",
		"      value: list\n      vocabulary: topic\n      guidance: Free-text", 1)), 0o644)
	s, err := schema.Load(tax)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := Pages(s)
	if err != nil {
		t.Fatal(err)
	}
	if want := "tags: [<garden | hiring | house | infra>]   # optional"; !strings.Contains(pages["note"], want) {
		t.Errorf("note.md lacks %q", want)
	}
}

func TestExcludesRendersOnTheFieldsEntry(t *testing.T) {
	dir := testutil.CopyExample(t)
	tax := filepath.Join(dir, "taxonomy")
	p := filepath.Join(tax, "traits.yaml")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), "      value: list\n      guidance: Free-text",
		"      value: list\n      excludes: topic\n      guidance: Free-text", 1)), 0o644)
	s, err := schema.Load(tax)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := Pages(s)
	if err != nil {
		t.Fatal(err)
	}
	if want := "for anything you filter on.\n\nNever repeats `topic`. Checked."; !strings.Contains(pages["note"], want) {
		t.Errorf("note.md lacks %q", want)
	}
}

func TestADirectoryReferenceRendersAsDirectories(t *testing.T) {
	dir := testutil.CopyExample(t)
	tax := filepath.Join(dir, "taxonomy")
	p := filepath.Join(tax, "singletons.yaml")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), "reference: people\n", "reference: people/*\n", 1)), 0o644)
	s, err := schema.Load(tax)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := Pages(s)
	if err != nil {
		t.Fatal(err)
	}
	want := "Values: any directory under [`people/`](../../people/) that holds an `index.md`, by its directory name."
	if !strings.Contains(pages["note"], want) {
		t.Errorf("note.md lacks %q", want)
	}
}

func TestADirectoryRendersWhereThePageLives(t *testing.T) {
	cases := []struct{ directory, want string }{
		{"projects/{topic}", "The page lives at `projects/{topic}/<slug>.md`, filled from its own frontmatter, " +
			"and its slug is unique under [`projects/`](../../projects/). A page anywhere else fails the check."},
		{"projects/{status}", "The page lives at `projects/{status}/<slug>.md`, filled from its own frontmatter, " +
			"and its slug is unique under [`projects/`](../../projects/). A page with no `status` sits directly in " +
			"`projects/`. A page anywhere else fails the check."},
		{"projects", "The page lives at `projects/<slug>.md`, and its slug is unique under " +
			"[`projects/`](../../projects/). A page anywhere else fails the check."},
	}
	for _, c := range cases {
		dir := testutil.CopyExample(t)
		tax := filepath.Join(dir, "taxonomy")
		p := filepath.Join(tax, "types", "project-plan.yaml")
		b, _ := os.ReadFile(p)
		body := strings.Replace(string(b), "optional:\n- tags\n", "optional:\n- tags\n- status\n", 1)
		body = strings.Replace(body, "fields: {}", "directory: "+c.directory+"\nfields:\n  status: {value: str}", 1)
		os.WriteFile(p, []byte(body), 0o644)
		s, err := schema.Load(tax)
		if err != nil {
			t.Fatal(err)
		}
		pages, err := Pages(s)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(pages["project-plan"], "\n"+c.want+"\n") {
			t.Errorf("%s: project-plan.md lacks %q", c.directory, c.want)
		}
		if strings.Contains(pages["note"], "lives at") {
			t.Errorf("%s: note.md, which declares no directory, says where it lives", c.directory)
		}
	}
}

func TestThePagesNameTheRepoInvocation(t *testing.T) {
	cases := []struct{ invocation, docs, check string }{
		{"", "taxidermist docs", "taxidermist check"},
		{"bin/kb/taxidermist", "bin/kb/taxidermist docs", "bin/kb/taxidermist check"},
		{"tools/taxidermist {command} --taxonomy kb/taxonomy",
			"tools/taxidermist docs --taxonomy kb/taxonomy", "tools/taxidermist check --taxonomy kb/taxonomy"},
	}
	for _, c := range cases {
		dir := testutil.CopyExample(t)
		tax := filepath.Join(dir, "taxonomy")
		if c.invocation != "" {
			p := filepath.Join(tax, "scope.yaml")
			b, _ := os.ReadFile(p)
			os.WriteFile(p, append(b, []byte("invocation: "+c.invocation+"\n")...), 0o644)
		}
		s, err := schema.Load(tax)
		if err != nil {
			t.Fatal(err)
		}
		pages, err := Pages(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"run `" + c.docs + "`. -->", "then run `" + c.docs + "`. `" + c.check + "` fails otherwise."} {
			if !strings.Contains(pages["README"], want) {
				t.Errorf("%q: README.md lacks %q", c.invocation, want)
			}
		}
		if want := "`" + c.check + "` fails otherwise."; !strings.Contains(pages["note"], want) {
			t.Errorf("%q: note.md lacks %q", c.invocation, want)
		}
	}
}

func TestTheSchemaLinkNamesTheDirectory(t *testing.T) {
	for _, readme := range []bool{true, false} {
		dir := testutil.CopyExample(t)
		tax := filepath.Join(dir, "taxonomy")
		want := "[taxonomy/](../README.md)"
		if !readme {
			os.Remove(filepath.Join(tax, "README.md"))
			want = "[taxonomy/](..)"
		}
		s, err := schema.Load(tax)
		if err != nil {
			t.Fatal(err)
		}
		pages, err := Pages(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, page := range []string{"README", "note"} {
			if !strings.Contains(pages[page], "under "+want) || strings.Contains(pages[page], "[..]") {
				t.Errorf("readme %v: %s.md lacks %q or still links [..]", readme, page, want)
			}
		}
	}
}

func TestYAMLPagesNameTheFilesAndCopyAWholeFileEntryOrItem(t *testing.T) {
	s, err := schema.Load(filepath.Join(testutil.Example(), "taxonomy"))
	if err != nil {
		t.Fatal(err)
	}
	pages, err := Pages(s)
	if err != nil {
		t.Fatal(err)
	}
	for page, want := range map[string]string{
		"site":      "# Format of `config/site.yaml`\n",
		"team":      "```yaml\n<name>:\n  purpose: <str>\n  lead: <str>\n  topics: [<garden | hiring | house | infra>]   # optional\n```\n",
		"rota-week": "```yaml\n- week: <str>\n  team: <str>\n  person: <str>\n```\n",
	} {
		if !strings.Contains(pages[page], want) {
			t.Errorf("%s.md lacks %q:\n%s", page, want, pages[page])
		}
		if strings.Contains(pages[page], "---\n") {
			t.Errorf("%s.md has frontmatter fences", page)
		}
	}
	for page, wants := range map[string][]string{
		"site": {"labels:   # optional\n  <name>: <str>", "· matches `^[A-Z][A-Za-z_]+/[A-Za-z_]+$`",
			"Shared vocabulary [`team`](vocabularies/team.md), read from [`config/teams.yaml`](../../config/teams.yaml).",
			"| `platform` | Keeps the servers and the build running. |"},
		"team":               {"One entry per top-level key, each with the fields below. The key matches `^[a-z][a-z-]*$`."},
		"rota-week":          {"Values: any top-level key of [`config/teams.yaml`](../../config/teams.yaml)."},
		"vocabularies/team":  {"# Vocabulary `team`", "Read from [`config/teams.yaml`](../../../config/teams.yaml): its top-level keys, each meaning its entry's `purpose:`."},
		"vocabularies/topic": {"Defined in [../../vocabularies/topic.yaml](../../vocabularies/topic.yaml).", "| `garden` | home | Anything that grows. |  |"},
		"README":             {"- [`config/teams.yaml`](team.md)", "- [`topic`](vocabularies/topic.md)"},
	} {
		for _, w := range wants {
			if !strings.Contains(pages[page], w) {
				t.Errorf("%s.md lacks %q", page, w)
			}
		}
	}
}

func TestAMapOfObjectsRendersOneEntryUnderAPlaceholder(t *testing.T) {
	dir := testutil.CopyExample(t)
	tax := filepath.Join(dir, "taxonomy")
	testutil.Write(t, tax, "types/site.yaml", "files: config/site.yaml\nrequired: [name, timezone, groups]\nfields:\n"+
		"  groups:\n    value: map\n    schema:\n      meaning: {value: str, required: true, guidance: g}\n"+
		"      read_by: {value: list, guidance: g}\n    guidance: g\n")
	s, err := schema.Load(tax)
	if err != nil {
		t.Fatal(err)
	}
	pages, _ := Pages(s)
	want := "groups:\n  <name>:\n    meaning: <str>\n    read_by: []   # optional\n"
	if !strings.Contains(pages["site"], want) {
		t.Errorf("site.md lacks %q:\n%s", want, pages["site"])
	}
}

func TestAStaleVocabularyPageIsPruned(t *testing.T) {
	dir := testutil.CopyExample(t)
	tax := filepath.Join(dir, "taxonomy")
	stray := testutil.Write(t, tax, "format/vocabularies/gone.md", "old\n")
	s, err := schema.Load(tax)
	if err != nil {
		t.Fatal(err)
	}
	if stale, _, _, _ := Sync(s, true); len(stale) != 1 || stale[0] != stray {
		t.Errorf("check reported %v", stale)
	}
	if _, _, _, err := Sync(s, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Error("stray vocabulary page survived a write")
	}
}
