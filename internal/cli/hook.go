package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/check"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

func runHook(args []string, stdout, stderr io.Writer) int {
	fs, tax := newFlags("hook", `taxidermist hook

The pre-commit gate. Checks only; it never rewrites a file.
  1. when anything under the taxonomy is staged: format/ must be current (docs --check)
  2. the staged markdown must match the schema (check, on those files only)

Scoped to staged files on purpose: a repo-wide gate would block every commit until
the whole backlog is fixed, which is how a hook earns a permanent --no-verify.
Audit the backlog with "taxidermist check".`, stderr)
	if _, err := parse(fs, args); err != nil {
		return flagExit(err)
	}
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintln(stderr, "taxidermist hook: not inside a git work tree:", err)
		return 2
	}
	top = strings.TrimSpace(top)
	out, err := git("diff", "--cached", "--name-only", "--diff-filter=ACMRD", "-z")
	if err != nil {
		fmt.Fprintln(stderr, "taxidermist hook:", err)
		return 2
	}
	taxAbs, _ := filepath.Abs(*tax)
	var staged []string
	taxChanged := false
	for _, name := range strings.Split(out, "\x00") {
		if name == "" {
			continue
		}
		p := filepath.Join(top, filepath.FromSlash(name))
		if p == taxAbs || strings.HasPrefix(p, taxAbs+string(filepath.Separator)) {
			taxChanged = true
		}
		if strings.HasSuffix(p, ".md") {
			staged = append(staged, p)
		}
	}
	if !taxChanged && len(staged) == 0 {
		return 0
	}
	s, ok := load(*tax, stderr)
	if !ok {
		return 2
	}
	code := 0
	if taxChanged {
		if docsSync(s, true, io.Discard, stderr) != 0 {
			fmt.Fprintf(stderr, "\ncommit blocked: %s/format/ is stale vs the schema.\n"+
				"  -> taxidermist docs --taxonomy %s   then re-stage, or   git commit --no-verify\n\n",
				schema.Display(s.Dir), *tax)
			code = 1
		}
	}
	if len(staged) > 0 {
		paths, err := check.Files(s, staged)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		r := check.New(s).Run(paths)
		defer writeWarnings(stderr, s, r, false)
		if len(r.Faults) > 0 {
			writeText(io.Discard, stderr, s, r, false)
			fmt.Fprintf(stderr, "\ncommit blocked: staged files do not match the schema in %s/.\n"+
				"  -> [value] reuse a listed value, or add the new one with its one-line definition in this commit\n"+
				"  -> [missing/unknown] the doctype's field set is required/optional in its types/ file\n"+
				"  -> [type/shape] the field's declared type is its `value:`\n"+
				"  -> [segment] move the file under a folder the vocabulary defines, or define the folder's name\n"+
				"  -> or   git commit --no-verify\n", schema.Display(s.Dir))
			code = 1
		}
	}
	return code
}

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}
