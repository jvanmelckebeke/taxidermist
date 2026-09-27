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
			"| `garden` | home | Anything that grows. |", "  open: <bool>", "  when: <date>   # optional"},
		"meeting":      {"  - what: <str>", "    done: <bool>   # optional", "[`people/`](../../people/)"},
		"project-plan": {"type: Project Plan"},
		"README":       {"- [`Project Plan`](project-plan.md)", "**Frontmatter describes the document.**"},
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
