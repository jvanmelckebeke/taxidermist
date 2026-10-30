package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/check"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

func runHook(args []string, stdout, stderr io.Writer) int {
	fs, tax := newFlags("hook", `taxidermist hook

The pre-commit gate. Checks only; it never rewrites a file.
  1. when anything under the taxonomy, or a data file a vocabulary reads with from:,
     is staged: format/ must be current (docs --check)
  2. the staged markdown and YAML must match the schema (check, on those files only)

Scoped to staged files on purpose: a repo-wide gate would block every commit until
the whole backlog is fixed, which is how a hook earns a permanent --no-verify.
Audit the backlog with "taxidermist check".`, stderr)
	if _, err := parse(fs, args); err != nil {
		return flagExit(err)
	}
	staged, taxChanged, ok := stagedFiles(*tax, stderr)
	if !ok {
		return 2
	}
	if !taxChanged && len(staged) == 0 {
		return 0
	}
	s, ok := load(*tax, stderr)
	if !ok {
		return 2
	}
	// A vocabulary read with `from:` is rendered from its data file, so a change to
	// that file can leave format/ stale without touching the taxonomy.
	for _, v := range s.Vocabularies {
		if v.Source != nil {
			taxChanged = taxChanged || slices.Contains(staged, filepath.Join(s.Base, filepath.FromSlash(v.Source.From)))
		}
	}
	code := 0
	if taxChanged {
		code = max(code, pagesGate(s, *tax, stderr))
	}
	if len(staged) > 0 {
		c := filesGate(s, staged, stderr)
		if c == 2 {
			return 2
		}
		code = max(code, c)
	}
	return code
}

// stagedFiles lists the staged markdown and YAML files, absolute, and whether
// anything under the taxonomy is staged. It reports to stderr when git fails.
func stagedFiles(tax string, stderr io.Writer) (staged []string, taxChanged, ok bool) {
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintln(stderr, "taxidermist hook: not inside a git work tree:", err)
		return nil, false, false
	}
	top = strings.TrimSpace(top)
	out, err := git("diff", "--cached", "--name-only", "--diff-filter=ACMRD", "-z")
	if err != nil {
		fmt.Fprintln(stderr, "taxidermist hook:", err)
		return nil, false, false
	}
	taxAbs, _ := filepath.Abs(tax)
	for _, name := range strings.Split(out, "\x00") {
		if name == "" {
			continue
		}
		p := filepath.Join(top, filepath.FromSlash(name))
		if p == taxAbs || strings.HasPrefix(p, taxAbs+string(filepath.Separator)) {
			taxChanged = true
		}
		if strings.HasSuffix(p, ".md") || schema.IsYAMLPath(p) {
			staged = append(staged, p)
		}
	}
	return staged, taxChanged, true
}

// pagesGate blocks the commit when format/ is stale.
func pagesGate(s *schema.Schema, tax string, stderr io.Writer) int {
	if docsSync(s, true, io.Discard, stderr) == 0 {
		return 0
	}
	// The hint repeats the hook's own --taxonomy unless the invocation already names one.
	docs := s.Command("docs")
	if !strings.Contains(docs, "--taxonomy") {
		docs += " --taxonomy " + tax
	}
	fmt.Fprintf(stderr, "\ncommit blocked: %s/format/ is stale vs the schema.\n"+
		"  -> %s   then re-stage, or   git commit --no-verify\n\n",
		schema.Display(s.Dir), docs)
	return 1
}

// filesGate blocks the commit when a staged file breaks the schema. Its warnings
// print last and never block.
func filesGate(s *schema.Schema, staged []string, stderr io.Writer) int {
	paths, err := check.Files(s, staged)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	r := check.New(s).Run(paths)
	defer writeWarnings(stderr, s, r, false)
	if len(r.Faults) == 0 {
		return 0
	}
	writeText(io.Discard, stderr, s, r, false)
	fmt.Fprintf(stderr, "\ncommit blocked: staged files do not match the schema in %s/.\n"+
		"  -> [value] reuse a listed value, or add the new one with its one-line definition in this commit\n"+
		"  -> [missing/unknown] the doctype's field set is required/optional in its types/ file\n"+
		"  -> [type/shape] the field's declared type is its `value:`\n"+
		"  -> [segment] move the file under a folder the vocabulary defines, or define the folder's name\n"+
		"  -> or   git commit --no-verify\n", schema.Display(s.Dir))
	return 1
}

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}
