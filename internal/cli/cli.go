// Package cli is the taxidermist command line.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/docs"
	"github.com/jvanmelckebeke/taxidermist/internal/lint"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

// Version is set at build time.
var Version = "dev"

const usage = `taxidermist: keep markdown frontmatter and YAML files to a declared schema.

The schema is a taxonomy directory (default ./taxonomy): scope.yaml, traits.yaml,
singletons.yaml, exempt.yaml, vocabularies/ and one types/<doctype>.yaml per doctype.

Usage:
  taxidermist check [FILE ...]   check frontmatter and YAML files against the schema (default: every governed root)
  taxidermist docs [--check]     generate taxonomy/format/, one how-to page per doctype
  taxidermist lint               check the schema against its own invariants
  taxidermist hook               the pre-commit gate: docs --check and check, on staged files
  taxidermist version

Every command takes --taxonomy DIR. Run "taxidermist <command> -h" for its flags.
`

// Run executes one command and returns the exit code: 0 clean, 1 faults, 2 a usage
// or schema error.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "check":
		return runCheck(rest, stdin, stdout, stderr)
	case "docs":
		return runDocs(rest, stdout, stderr)
	case "lint":
		return runLint(rest, stdout, stderr)
	case "hook":
		return runHook(rest, stdout, stderr)
	case "version", "--version":
		fmt.Fprintln(stdout, "taxidermist", Version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "taxidermist: unknown command %q\n\n%s", cmd, usage)
	return 2
}

func newFlags(name, summary string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	tax := fs.String("taxonomy", "taxonomy", "the taxonomy directory")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "%s\n\nFlags:\n", summary)
		fs.PrintDefaults()
	}
	return fs, tax
}

// parse accepts flags before, between and after positional arguments. Everything
// after a `--` is positional.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			return append(pos, rest...), nil
		}
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

func load(dir string, stderr io.Writer) (*schema.Schema, bool) {
	s, err := schema.Load(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, false
	}
	return s, true
}

func flagExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

func runDocs(args []string, stdout, stderr io.Writer) int {
	fs, tax := newFlags("docs", `taxidermist docs [--check]

Generate <taxonomy>/format/: one page per doctype with a fill-in template, every
field's guidance and every vocabulary it uses, one page per vocabulary under
vocabularies/, plus a README index. The index opens
with <taxonomy>/`+docs.HouseRules+` when that file exists. Pages no doctype generates
are deleted.`, stderr)
	chk := fs.Bool("check", false, "exit 1 if any page is stale; write nothing")
	if _, err := parse(fs, args); err != nil {
		return flagExit(err)
	}
	s, ok := load(*tax, stderr)
	if !ok {
		return 2
	}
	return docsSync(s, *chk, stdout, stderr)
}

func docsSync(s *schema.Schema, chk bool, stdout, stderr io.Writer) int {
	stale, written, total, err := docs.Sync(s, chk)
	if err != nil {
		fmt.Fprintln(stderr, "taxidermist:", err)
		return 2
	}
	out := schema.Display(filepath.Join(s.Dir, "format"))
	if chk {
		if len(stale) > 0 {
			fmt.Fprintf(stderr, "%s is stale vs the schema:\n", out)
			for _, p := range stale {
				fmt.Fprintf(stderr, "  %s\n", schema.Display(p))
			}
			return 1
		}
		fmt.Fprintf(stdout, "%s: %d page(s) current.\n", out, total)
		return 0
	}
	fmt.Fprintf(stdout, "%s: %d page(s) written, %d total.\n", out, written, total)
	return 0
}

func runLint(args []string, stdout, stderr io.Writer) int {
	var doc strings.Builder
	doc.WriteString("taxidermist lint\n\nCheck the schema against its own invariants:\n")
	for _, r := range lint.Rules {
		fmt.Fprintf(&doc, "  %-27s %s\n", r.ID, r.Doc)
	}
	fs, tax := newFlags("lint", strings.TrimRight(doc.String(), "\n"), stderr)
	if _, err := parse(fs, args); err != nil {
		return flagExit(err)
	}
	s, ok := load(*tax, stderr)
	if !ok {
		return 2
	}
	problems := lint.Run(s)
	if len(problems) == 0 {
		fmt.Fprintf(stdout, "taxidermist lint: %s passes every rule.\n", schema.Display(s.Dir))
		return 0
	}
	fmt.Fprintf(stderr, "taxidermist lint: %d problem(s) in %s:\n", len(problems), schema.Display(s.Dir))
	for _, p := range problems {
		fmt.Fprintf(stderr, "  [%s] %s\n", p.Rule, p.Msg)
	}
	return 1
}
