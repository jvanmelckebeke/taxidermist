package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/check"
)

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs, tax := newFlags("check", `taxidermist check [FILE ...]

Check frontmatter against the schema. With no files, every root in scope.yaml is
audited. Files outside the governed roots are dropped, so a caller can hand over
every staged markdown file and scope.yaml decides which ones the schema governs.`, stderr)
	facets := fs.Bool("facets", false, "print every key and value in use per doctype, drift first")
	ungoverned := fs.Bool("ungoverned", false, "list the files whose doctype has no schema, instead of tallying them")
	kinds := fs.String("kind", "", "only report these kinds, comma-separated: "+strings.Join(check.Kinds, ", ")+", mismatch")
	verbose := fs.Bool("verbose", false, "list every warning and segment fault per file, not once per folder")
	asJSON := fs.Bool("json", false, "JSON output")
	asTOON := fs.Bool("toon", false, "TOON output: token-efficient, one row per fault")
	files, err := parse(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if *asJSON && *asTOON {
		fmt.Fprintln(stderr, "taxidermist: --json and --toon are exclusive")
		return 2
	}
	s, ok := load(*tax, stderr)
	if !ok {
		return 2
	}
	paths, err := check.Files(s, files)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *facets {
		check.Facets(stdout, s, paths)
		return 0
	}
	r := check.New(s).Run(paths)
	if *ungoverned {
		printUngoverned(stdout, r)
		return 0
	}
	if *kinds != "" {
		want := map[string]bool{}
		for _, k := range strings.Split(*kinds, ",") {
			want[strings.TrimSpace(k)] = true
		}
		r.Faults, r.Warnings = only(r.Faults, want), only(r.Warnings, want)
	}
	switch {
	case *asJSON:
		writeJSON(stdout, r)
	case *asTOON:
		writeTOON(stdout, r)
	default:
		writeText(stdout, stderr, s, r, *verbose)
		writeWarnings(stderr, s, r, *verbose)
	}
	if len(r.Faults) > 0 {
		return 1
	}
	return 0
}

func only(fs []check.Fault, want map[string]bool) []check.Fault {
	var kept []check.Fault
	for _, f := range fs {
		if want[f.Kind] {
			kept = append(kept, f)
		}
	}
	return kept
}
