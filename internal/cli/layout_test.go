package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jvanmelckebeke/taxidermist/internal/testutil"
)

// oldLayout moves a copy of the example back to taxonomy/ with its scope.yaml inside.
func oldLayout(t *testing.T) string {
	t.Helper()
	dir := testutil.CopyExample(t)
	for _, mv := range [][2]string{{".taxidermist", "taxonomy"}, {".taxidermist.yaml", "taxonomy/scope.yaml"}} {
		if err := os.Rename(filepath.Join(dir, mv[0]), filepath.Join(dir, mv[1])); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const moveHint = "taxonomy/scope.yaml is the old layout; see the README's Config file section"

func TestTheNewLayoutIsFoundFromTheWorkingDirectory(t *testing.T) {
	t.Chdir(testutil.CopyExample(t))
	code, _, errs := run(t, "check")
	if code != 0 || strings.Contains(errs, "old layout") {
		t.Errorf("exit %d\n%s", code, errs)
	}
}

func TestTheOldLayoutStillWorksAndSaysToMove(t *testing.T) {
	dir := oldLayout(t)
	t.Chdir(dir)
	// The old layout writes its pages to taxonomy/format.
	if code, _, errs := run(t, "docs"); code != 0 {
		t.Fatalf("docs: exit %d\n%s", code, errs)
	}
	for _, args := range [][]string{{"check"}, {"check", "--taxonomy", "taxonomy"}, {"lint"}, {"docs", "--check"}} {
		code, out, errs := run(t, args...)
		if code != 0 || !strings.Contains(errs, moveHint) {
			t.Errorf("%v: exit %d\n%s%s", args, code, out, errs)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "taxonomy", "format", "README.md")); err != nil {
		t.Error("the old layout did not write taxonomy/format:", err)
	}
}

func TestBothLayoutsAtOnceIsASchemaError(t *testing.T) {
	dir := oldLayout(t)
	testutil.Write(t, dir, ".taxidermist.yaml", "roots: [notes]\n")
	t.Chdir(dir)
	// Without the flag the config in the working directory names the default
	// folder, which does not exist, so point it at taxonomy/.
	testutil.Write(t, dir, ".taxidermist.yaml", "taxonomy: taxonomy\nroots: [notes]\n")
	for _, args := range [][]string{{"check"}, {"check", "--taxonomy", "taxonomy"}} {
		code, _, errs := run(t, args...)
		if code != 2 || !strings.Contains(errs, "both .taxidermist.yaml and taxonomy/scope.yaml exist") {
			t.Errorf("%v: exit %d\n%s", args, code, errs)
		}
	}
}

func TestTheConfigNamesTheSchemaFolder(t *testing.T) {
	dir := testutil.CopyExample(t)
	if err := os.Rename(filepath.Join(dir, ".taxidermist"), filepath.Join(dir, "schema")); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, ".taxidermist.yaml")
	b, _ := os.ReadFile(cfg)
	os.WriteFile(cfg, append([]byte("taxonomy: schema\n"), b...), 0o644)
	t.Chdir(dir)
	if code, _, errs := run(t, "check"); code != 0 {
		t.Errorf("exit %d\n%s", code, errs)
	}
}

func TestNoConfigAnywhereSaysSo(t *testing.T) {
	t.Chdir(t.TempDir())
	code, _, errs := run(t, "check")
	if code != 2 || !strings.Contains(errs, "Pass --taxonomy DIR") {
		t.Errorf("exit %d\n%s", code, errs)
	}
}

func TestAnUnknownConfigKeyIsASchemaError(t *testing.T) {
	dir := testutil.CopyExample(t)
	cfg := filepath.Join(dir, ".taxidermist.yaml")
	b, _ := os.ReadFile(cfg)
	os.WriteFile(cfg, append(b, "docz: x\n"...), 0o644)
	t.Chdir(dir)
	code, _, errs := run(t, "check")
	if code != 2 || !strings.Contains(errs, "unknown key `docz`") {
		t.Errorf("exit %d\n%s", code, errs)
	}
}

func TestDocsGoWhereTheConfigSays(t *testing.T) {
	dir := testutil.CopyExample(t)
	os.RemoveAll(filepath.Join(dir, ".taxidermist", "format"))
	cfg := filepath.Join(dir, ".taxidermist.yaml")
	b, _ := os.ReadFile(cfg)
	// A root of `.` would walk the docs folder if check did not skip it.
	os.WriteFile(cfg, []byte(strings.Replace(string(b), "roots:\n", "docs: formatting-docs\nroots:\n- .\n", 1)), 0o644)
	t.Chdir(dir)
	if code, out, errs := run(t, "docs"); code != 0 || !strings.Contains(out, "formatting-docs: 10 page(s) written") {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(dir, ".taxidermist", "format")); err == nil {
		t.Error("pages were also written to the default folder")
	}
	pages := filepath.Join(dir, "formatting-docs")
	// The links climb out of the new folder, not out of format/.
	for page, want := range map[string]string{"site.md": "](../config/teams.yaml)", "note.md": "](../.taxidermist/traits.yaml)"} {
		if got, _ := os.ReadFile(filepath.Join(pages, page)); !strings.Contains(string(got), want) {
			t.Errorf("%s lacks %q", page, want)
		}
	}
	if code, _, errs := run(t, "docs", "--check"); code != 0 {
		t.Errorf("docs --check: exit %d\n%s", code, errs)
	}
	// Under root `.` the 10 generated pages would make it 31 files.
	code, out, errs := run(t, "check")
	if code != 0 || !strings.Contains(out, "21 governed file(s) checked") {
		t.Errorf("check: exit %d\n%s%s", code, out, errs)
	}
}

func TestDocsRefuseAFolderWithHandWrittenMarkdown(t *testing.T) {
	dir := testutil.CopyExample(t)
	cfg := filepath.Join(dir, ".taxidermist.yaml")
	b, _ := os.ReadFile(cfg)
	os.WriteFile(cfg, append([]byte("docs: guides\n"), b...), 0o644)
	mine := testutil.Write(t, dir, "guides/setup.md", "# By hand\n")
	t.Chdir(dir)
	for _, args := range [][]string{{"docs"}, {"docs", "--check"}} {
		code, _, errs := run(t, args...)
		if code != 2 || !strings.Contains(errs, "hand-written markdown") {
			t.Errorf("%v: exit %d\n%s", args, code, errs)
		}
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("docs deleted a hand-written page:", err)
	}
	if code, _, errs := run(t, "lint"); code != 1 || !strings.Contains(errs, "[docs-folder-generated]") {
		t.Errorf("lint: exit %d\n%s", code, errs)
	}
}

func TestDocsCheckWithNoDocsFolderIsClean(t *testing.T) {
	dir := testutil.CopyExample(t)
	os.RemoveAll(filepath.Join(dir, ".taxidermist", "format"))
	t.Chdir(dir)
	code, out, _ := run(t, "docs", "--check")
	if code != 0 || !strings.Contains(out, ".taxidermist/format: not generated") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestADocsFolderThatIsARootOrTheSchemaIsRefused(t *testing.T) {
	for _, docs := range []string{".taxidermist", "notes", "."} {
		dir := testutil.CopyExample(t)
		cfg := filepath.Join(dir, ".taxidermist.yaml")
		b, _ := os.ReadFile(cfg)
		os.WriteFile(cfg, append([]byte("docs: "+docs+"\n"), b...), 0o644)
		t.Chdir(dir)
		if code, _, errs := run(t, "check"); code != 2 || !strings.Contains(errs, "Pick a folder of its own") {
			t.Errorf("docs: %s: exit %d\n%s", docs, code, errs)
		}
	}
}
