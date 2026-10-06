package cli

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
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
	if code != 1 || !strings.Contains(errs, `[value] topic: "nope"`) || !strings.Contains(errs, "commit blocked") {
		t.Fatalf("bad note: exit %d\n%s", code, errs)
	}
	gitIn(t, dir, "reset", "-q")

	// A rename into a folder the vocabulary does not define is blocked.
	gitIn(t, dir, "mv", "projects/infra/build-cache.md", "projects/build-cache.md")
	code, _, errs = run(t, "hook")
	if code != 1 || !strings.Contains(errs, `[segment] projects/*: "build-cache"`) {
		t.Fatalf("rename into an undefined folder: exit %d\n%s", code, errs)
	}
	gitIn(t, dir, "reset", "-q", "--hard")

	// A field that disagrees with its folder warns and does not block.
	testutil.Write(t, dir, "projects/infra/x.md", "---\ntype: Project Plan\ntitle: x\ntopic: hiring\n---\n")
	gitIn(t, dir, "add", "projects/infra/x.md")
	code, _, errs = run(t, "hook")
	if code != 0 || !strings.Contains(errs, "1 warning(s)") || !strings.Contains(errs, "projects/infra    1 file(s) say topic: hiring 1") {
		t.Fatalf("mismatch: exit %d\n%s", code, errs)
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

func TestWarningsSummarisePerFolder(t *testing.T) {
	dir := testutil.CopyExample(t)
	t.Chdir(dir)
	for i, topic := range []string{"hiring", "hiring", "garden"} {
		testutil.Write(t, dir, "projects/infra/w"+strconv.Itoa(i)+".md", "---\ntype: Project Plan\ntitle: w\ntopic: "+topic+"\n---\n")
	}
	code, _, errs := run(t, "check")
	if code != 0 || !strings.Contains(errs, "3 warning(s)") ||
		!strings.Contains(errs, "projects/infra    3 file(s) say topic: hiring 2, garden 1") ||
		strings.Contains(errs, "w0.md") {
		t.Errorf("summary: exit %d\n%s", code, errs)
	}
	if _, _, errs := run(t, "check", "--verbose"); !strings.Contains(errs, "projects/infra/w0.md") ||
		!strings.Contains(errs, "[mismatch] topic: \"hiring\"") {
		t.Errorf("verbose:\n%s", errs)
	}
	if _, out, _ := run(t, "check", "--json"); !strings.Contains(out, `"kind": "mismatch"`) {
		t.Errorf("json:\n%s", out)
	}
	if _, out, _ := run(t, "check", "--toon"); !strings.Contains(out, "warnings[3]{file,kind,field,value,why}:") {
		t.Errorf("toon:\n%s", out)
	}
	if _, _, errs := run(t, "check", "--kind", "segment"); strings.Contains(errs, "warning") {
		t.Errorf("--kind segment kept warnings:\n%s", errs)
	}
}

func TestSegmentFaultsPrintOncePerFolder(t *testing.T) {
	dir := testutil.CopyExample(t)
	t.Chdir(dir)
	for _, n := range []string{"a", "b", "c"} {
		testutil.Write(t, dir, "projects/foobar/"+n+".md", "---\ntype: Project Plan\ntitle: x\ntopic: infra\n---\n")
	}
	code, _, errs := run(t, "check")
	if code != 1 || strings.Count(errs, "[segment]") != 1 || !strings.Contains(errs, "projects/foobar/  (3 file(s))") {
		t.Errorf("summary: exit %d\n%s", code, errs)
	}
	if _, _, errs := run(t, "check", "--verbose"); strings.Count(errs, "[segment]") != 3 {
		t.Errorf("verbose:\n%s", errs)
	}
}
