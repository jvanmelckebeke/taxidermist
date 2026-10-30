package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jvanmelckebeke/taxidermist"
	"github.com/jvanmelckebeke/taxidermist/internal/check"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

func runCheck(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs, tax := newFlags("check", `taxidermist check [FILE ...]
taxidermist check --stdin-filename PATH < FILE

Check frontmatter, and the YAML files a doctype's files: names, against the schema.
With no files, every root in scope.yaml is audited. Files outside the governed roots
are dropped, so a caller can hand over every staged file and scope.yaml decides
which ones the schema governs.

--stdin-filename checks the content on stdin as the markdown or YAML file at PATH,
which need not exist. PATH must sit under a governed root.`, stderr)
	facets := fs.Bool("facets", false, "print every key and value in use per doctype, drift first")
	ungoverned := fs.Bool("ungoverned", false, "list the files whose doctype has no schema, instead of tallying them")
	kinds := fs.String("kind", "", "only report these kinds, comma-separated: "+strings.Join(check.Kinds, ", ")+", mismatch")
	verbose := fs.Bool("verbose", false, "list every warning and segment fault per file, not once per folder")
	stdinName := fs.String("stdin-filename", "", "check the content on stdin as the file at this path")
	asJSON := fs.Bool("json", false, "JSON output, the shape the README documents")
	asTOON := fs.Bool("toon", false, "TOON output: token-efficient, one row per fault")
	files, err := parse(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if *asJSON && *asTOON {
		fmt.Fprintln(stderr, "taxidermist: --json and --toon are exclusive")
		return 2
	}
	if *stdinName != "" && (len(files) > 0 || *facets) {
		fmt.Fprintln(stderr, "taxidermist: --stdin-filename takes no FILE arguments and no --facets")
		return 2
	}
	var content []byte
	if *stdinName != "" {
		if content, err = io.ReadAll(stdin); err != nil {
			fmt.Fprintln(stderr, "taxidermist: reading stdin:", err)
			return 2
		}
	}
	want := kindSet(*kinds)
	if *asJSON && !*facets && !*ungoverned {
		return checkJSON(stdout, stderr, *tax, files, *stdinName, content, want)
	}
	s, ok := load(*tax, stderr)
	if !ok {
		return 2
	}
	var r *check.Result
	if *stdinName != "" {
		abs, err := check.Governed(s, *stdinName)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		r = check.New(s).Content(abs, content)
	} else {
		paths, err := check.Files(s, files)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		if *facets {
			check.Facets(stdout, s, paths)
			return 0
		}
		r = check.New(s).Run(paths)
	}
	if *ungoverned {
		printUngoverned(stdout, r)
		return 0
	}
	return report(stdout, stderr, s, r, want, *asTOON, *verbose)
}

// report prints a check's findings of the wanted kinds, as TOON or as text, and
// returns the exit code.
func report(stdout, stderr io.Writer, s *schema.Schema, r *check.Result, want map[string]bool, asTOON, verbose bool) int {
	if len(want) > 0 {
		r.Faults, r.Warnings = only(r.Faults, want), only(r.Warnings, want)
	}
	if asTOON {
		writeTOON(stdout, r)
	} else {
		writeText(stdout, stderr, s, r, verbose)
		writeWarnings(stderr, s, r, verbose)
	}
	if len(r.Faults) > 0 {
		return 1
	}
	return 0
}

// checkJSON runs the check through the public package, so the JSON output is the
// Result a Go caller gets.
func checkJSON(stdout, stderr io.Writer, dir string, files []string, stdinName string, content []byte, want map[string]bool) int {
	t, err := taxidermist.Load(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var r *taxidermist.Result
	if stdinName != "" {
		r, err = t.ValidateContent(stdinName, content)
	} else {
		r, err = t.Validate(files...)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if len(want) > 0 {
		r.Faults, r.Warnings = onlyFindings(r.Faults, want), onlyFindings(r.Warnings, want)
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(r)
	if !r.OK() {
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

func onlyFindings(fs []taxidermist.Finding, want map[string]bool) []taxidermist.Finding {
	kept := []taxidermist.Finding{}
	for _, f := range fs {
		if want[f.Kind] {
			kept = append(kept, f)
		}
	}
	return kept
}

// kindSet reads --kind's comma-separated list.
func kindSet(kinds string) map[string]bool {
	want := map[string]bool{}
	for _, k := range strings.Split(kinds, ",") {
		if k = strings.TrimSpace(k); k != "" {
			want[k] = true
		}
	}
	return want
}
