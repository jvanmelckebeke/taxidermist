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
	if total != len(s.Types)+1 {
		t.Errorf("%d pages for %d doctypes", total, len(s.Types))
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
