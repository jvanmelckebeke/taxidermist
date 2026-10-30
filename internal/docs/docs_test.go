package docs

import (
	"os"
	"path/filepath"
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
