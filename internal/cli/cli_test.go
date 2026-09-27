package cli

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jvanmelckebeke/taxidermist/internal/testutil"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestHook(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := testutil.CopyExample(t)
	t.Chdir(dir)
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-qm", "init")

	if code, _, errs := run(t, "hook"); code != 0 {
		t.Fatalf("nothing staged: exit %d\n%s", code, errs)
	}

	testutil.Write(t, dir, "elsewhere/x.md", "---\ntype: nonsense\n---\n")
	gitIn(t, dir, "add", "elsewhere/x.md")
	if code, _, errs := run(t, "hook"); code != 0 {
		t.Fatalf("ungoverned path staged: exit %d\n%s", code, errs)
	}

	testutil.Write(t, dir, "notes/bad.md", "---\ntype: note\ntitle: t\ntopic: nope\ndate: 2026-01-01\n---\n")
	gitIn(t, dir, "add", "notes/bad.md")
	code, _, errs := run(t, "hook")
	if code != 1 || !strings.Contains(errs, "[value] topic: 'nope'") || !strings.Contains(errs, "commit blocked") {
		t.Fatalf("bad note: exit %d\n%s", code, errs)
	}
	gitIn(t, dir, "reset", "-q")

	p := "taxonomy/types/person.yaml"
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), "How you know them.", "How you met.", 1)), 0o644)
	gitIn(t, dir, "add", p)
	if code, _, errs := run(t, "hook"); code != 1 || !strings.Contains(errs, "format/ is stale") {
		t.Fatalf("stale pages: exit %d\n%s", code, errs)
	}
	if code, _, errs := run(t, "docs"); code != 0 {
		t.Fatal(errs)
	}
	gitIn(t, dir, "add", "taxonomy")
	if code, _, errs := run(t, "hook"); code != 0 {
		t.Fatalf("after docs: exit %d\n%s", code, errs)
	}
}

func TestCheckOutputs(t *testing.T) {
	dir := testutil.CopyExample(t)
	t.Chdir(dir)
	testutil.Write(t, dir, "notes/bad.md", "---\ntype: note\ntitle: t\ntopic: nope, really\ndate: 2026-01-01\n---\n")
	code, out, _ := run(t, "check", "--toon")
	if code != 1 || !strings.Contains(out, `notes/bad.md,value,topic,"nope, really",`) {
		t.Errorf("toon: exit %d\n%s", code, out)
	}
	code, out, _ = run(t, "check", "notes/bad.md", "--json")
	if code != 1 || !strings.Contains(out, `"value": "nope, really"`) {
		t.Errorf("json: exit %d\n%s", code, out)
	}
	if code, _, _ := run(t, "check", "--kind", "missing"); code != 0 {
		t.Errorf("--kind missing should filter the value fault out, exit %d", code)
	}
	if code, _, errs := run(t, "check", "--json", "--toon"); code != 2 {
		t.Errorf("exclusive flags: exit %d %s", code, errs)
	}
}
