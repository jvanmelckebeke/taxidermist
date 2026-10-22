package taxidermist_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jvanmelckebeke/taxidermist"
	"github.com/jvanmelckebeke/taxidermist/internal/testutil"
)

func load(t *testing.T) (*taxidermist.Taxonomy, string) {
	t.Helper()
	dir := testutil.CopyExample(t)
	t.Chdir(dir)
	tax, err := taxidermist.Load("taxonomy")
	if err != nil {
		t.Fatal(err)
	}
	return tax, dir
}

func TestValidate(t *testing.T) {
	tax, dir := load(t)
	r, err := tax.Validate()
	if err != nil || !r.OK() || r.Governed == 0 {
		t.Fatalf("example: %v %+v", err, r)
	}
	testutil.Write(t, dir, "notes/bad.md", "---\ntype: note\ntitle: t\ntopic: nope\ndate: 2026-01-01\n---\n")
	r, err = tax.Validate("notes/bad.md", "elsewhere/x.md", "notes/missing.md")
	if err != nil || r.OK() || r.Governed != 1 || len(r.Faults) != 1 {
		t.Fatalf("bad note: %v %+v", err, r)
	}
	f := r.Faults[0]
	if f.File != "notes/bad.md" || f.Kind != "value" || f.Field != "topic" || f.Value == nil || *f.Value != "nope" || f.Why == "" {
		t.Errorf("finding: %+v", f)
	}
}

func TestValidateContent(t *testing.T) {
	tax, dir := load(t)
	r, err := tax.ValidateContent("notes/new.md", []byte("---\ntype: note\ntitle: t\n---\nbody\n"))
	if err != nil || r.OK() || r.Governed != 1 {
		t.Fatalf("%v %+v", err, r)
	}
	if f := r.Faults[0]; f.File != "notes/new.md" || f.Kind != "missing" || f.Value != nil {
		t.Errorf("finding: %+v", f)
	}
	// The content wins over what is on disk at that path.
	r, err = tax.ValidateContent(filepath.Join(dir, "notes/tomato-blight.md"), []byte("---\ntype: note\ntitle: t\ntopic: nope\ndate: 2026-01-01\n---\n"))
	if err != nil || len(r.Faults) != 1 || r.Faults[0].Field != "topic" {
		t.Fatalf("over an existing file: %v %+v", err, r)
	}
	if _, err := tax.ValidateContent("elsewhere/x.md", []byte("---\ntype: note\n---\n")); err == nil || !strings.Contains(err.Error(), "elsewhere/x.md") {
		t.Errorf("outside the roots: %v", err)
	}
}

func TestResultJSON(t *testing.T) {
	tax, _ := load(t)
	r, err := tax.ValidateContent("notes/new.md", []byte("---\ntype: note\ntitle: t\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	for _, want := range []string{`"governed":1`, `"warnings":[]`, `"ungoverned":{}`, `"file":"notes/new.md"`, `"kind":"missing"`, `"value":null`, `"why":`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
}

func TestSchemaErrors(t *testing.T) {
	dir := t.TempDir()
	testutil.Write(t, dir, "taxonomy/types/note.yaml", "required: [\n")
	if _, err := taxidermist.Load(filepath.Join(dir, "taxonomy")); err == nil {
		t.Error("a broken types/ file loaded")
	}
	tax, err := taxidermist.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tax.Validate(); err == nil || !strings.Contains(err.Error(), "no roots") {
		t.Errorf("no roots: %v", err)
	}
}
