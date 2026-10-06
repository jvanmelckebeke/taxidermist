// Package cli is the taxidermist command line.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/check"
	"github.com/jvanmelckebeke/taxidermist/internal/docs"
	"github.com/jvanmelckebeke/taxidermist/internal/lint"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

// Version is set at build time.
var Version = "dev"

const usage = `taxidermist: keep markdown frontmatter to a declared schema.

The schema is a taxonomy directory (default ./taxonomy): scope.yaml, traits.yaml,
singletons.yaml, exempt.yaml, vocabularies/ and one types/<doctype>.yaml per doctype.

Usage:
  taxidermist check [FILE ...]   check frontmatter against the schema (default: every governed root)
  taxidermist docs [--check]     generate taxonomy/format/, one how-to page per doctype
  taxidermist lint               check the schema against its own invariants
  taxidermist hook               the pre-commit gate: docs --check and check, on staged files
  taxidermist version

Every command takes --taxonomy DIR. Run "taxidermist <command> -h" for its flags.
`

// Run executes one command and returns the exit code: 0 clean, 1 faults, 2 a usage
// or schema error.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "check":
		return runCheck(rest, stdout, stderr)
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

// parse accepts flags before, between and after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
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

func flagExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

func printUngoverned(w io.Writer, r *check.Result) {
	for _, dt := range byCount(r.Ungoverned) {
		fmt.Fprintf(w, "\n%s  (%d files)\n", dt, len(r.Ungoverned[dt]))
		for _, p := range r.Ungoverned[dt] {
			fmt.Fprintf(w, "  %s\n", schema.Display(p))
		}
	}
}

func byCount(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if len(m[keys[i]]) != len(m[keys[j]]) {
			return len(m[keys[i]]) > len(m[keys[j]])
		}
		return keys[i] < keys[j]
	})
	return keys
}

func shown(v any) string {
	return value.Truncate(value.Text(v), 70)
}

// writeText prints the faults per file. A segment fault is about a folder, so unless
// verbose it prints once per folder with its file count rather than once per file.
func writeText(stdout, stderr io.Writer, s *schema.Schema, r *check.Result, verbose bool) {
	if len(r.Ungoverned) > 0 {
		n := 0
		var tally []string
		for _, dt := range byCount(r.Ungoverned) {
			n += len(r.Ungoverned[dt])
			tally = append(tally, fmt.Sprintf("%s %d", dt, len(r.Ungoverned[dt])))
		}
		fmt.Fprintf(stdout, "taxidermist: %d file(s) carry a doctype with no schema (%s).\n"+
			"  -> --ungoverned lists them; a doctype joins the schema by getting a types/ file.\n\n",
			n, strings.Join(tally, ", "))
	}
	if len(r.Faults) == 0 {
		fmt.Fprintf(stdout, "taxidermist: %d governed file(s) checked, every one matches the schema.\n", r.Governed())
		return
	}
	counts := map[string]int{}
	var order []string
	for _, f := range r.Faults {
		if counts[f.Kind] == 0 {
			order = append(order, f.Kind)
		}
		counts[f.Kind]++
	}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	var tally []string
	for _, k := range order {
		tally = append(tally, fmt.Sprintf("%s %d", k, counts[k]))
	}
	fmt.Fprintf(stderr, "taxidermist: %d schema fault(s) in %d governed file(s) (%s):\n",
		len(r.Faults), r.Governed(), strings.Join(tally, ", "))
	inFolder := map[string]int{}
	if !verbose {
		for _, f := range r.Faults {
			if f.Kind == "segment" {
				inFolder[f.Dir+"\x00"+f.Field]++
			}
		}
	}
	last := ""
	for _, f := range r.Faults {
		if n, ok := inFolder[f.Dir+"\x00"+f.Field]; ok && f.Kind == "segment" {
			if n == 0 {
				continue
			}
			inFolder[f.Dir+"\x00"+f.Field] = 0
			fmt.Fprintf(stderr, "  %s/  (%d file(s))\n", schema.Display(f.Dir), n)
			last = ""
		} else if f.Path != last {
			fmt.Fprintf(stderr, "  %s\n", schema.Display(f.Path))
			last = f.Path
		}
		val := ""
		if f.Value != nil {
			val = ": " + strconv.Quote(shown(f.Value))
		}
		fmt.Fprintf(stderr, "    [%s] %s%s\n      %s\n", f.Kind, f.Field, val, f.Why)
	}
	fmt.Fprintf(stderr, "\n%s/ is the schema. Either fix the file, or, if the concept genuinely fits\n"+
		"nothing there, add it WITH its one-line definition in the same change. The definition\n"+
		"is the declaration: a value with no definition does not exist.\n", schema.Display(s.Dir))
}

// writeWarnings summarises mismatch warnings as one line per folder, since a tree
// that files by folder and also tags by field can disagree in dozens of files at
// once. verbose lists every file instead.
func writeWarnings(w io.Writer, s *schema.Schema, r *check.Result, verbose bool) {
	if len(r.Warnings) == 0 {
		return
	}
	fmt.Fprintf(w, "\ntaxidermist: %d warning(s): the file's field differs from the folder it sits in. "+
		"Warnings never fail a check or block a commit.\n", len(r.Warnings))
	if verbose {
		last := ""
		for _, f := range r.Warnings {
			if f.Path != last {
				fmt.Fprintf(w, "  %s\n", schema.Display(f.Path))
				last = f.Path
			}
			fmt.Fprintf(w, "    [%s] %s: %s\n      %s\n", f.Kind, f.Field, strconv.Quote(shown(f.Value)), f.Why)
		}
		return
	}
	type folder struct {
		dir, field string
		n          int
		vals       map[string]int
		order      []string
	}
	var folders []*folder
	byDir := map[string]*folder{}
	for _, f := range r.Warnings {
		dir := schema.Display(f.Dir)
		key := dir + "\x00" + f.Field
		fo := byDir[key]
		if fo == nil {
			fo = &folder{dir: dir, field: f.Field, vals: map[string]int{}}
			byDir[key] = fo
			folders = append(folders, fo)
		}
		fo.n++
		v := shown(f.Value)
		if fo.vals[v] == 0 {
			fo.order = append(fo.order, v)
		}
		fo.vals[v]++
	}
	width := 0
	for _, fo := range folders {
		width = max(width, len(fo.dir))
	}
	for _, fo := range folders {
		sort.SliceStable(fo.order, func(i, j int) bool { return fo.vals[fo.order[i]] > fo.vals[fo.order[j]] })
		var parts []string
		for i, v := range fo.order {
			if i == 5 {
				parts = append(parts, fmt.Sprintf("+%d more", len(fo.order)-5))
				break
			}
			parts = append(parts, fmt.Sprintf("%s %d", v, fo.vals[v]))
		}
		fmt.Fprintf(w, "  %-*s  %3d file(s) say %s: %s\n", width, fo.dir, fo.n, fo.field, strings.Join(parts, ", "))
	}
	fmt.Fprintln(w, "  -> taxidermist check --verbose lists each file")
}

type jsonFault struct {
	File  string  `json:"file"`
	Kind  string  `json:"kind"`
	Field string  `json:"field"`
	Value *string `json:"value"`
	Why   string  `json:"why"`
}

func writeJSON(w io.Writer, r *check.Result) {
	out := struct {
		Governed   int                 `json:"governed"`
		Faults     []jsonFault         `json:"faults"`
		Warnings   []jsonFault         `json:"warnings"`
		Ungoverned map[string][]string `json:"ungoverned"`
	}{Governed: r.Governed(), Faults: []jsonFault{}, Warnings: []jsonFault{}, Ungoverned: map[string][]string{}}
	conv := func(f check.Fault) jsonFault {
		jf := jsonFault{File: schema.Display(f.Path), Kind: f.Kind, Field: f.Field, Why: f.Why}
		if f.Value != nil {
			v := value.Text(f.Value)
			jf.Value = &v
		}
		return jf
	}
	for _, f := range r.Faults {
		out.Faults = append(out.Faults, conv(f))
	}
	for _, f := range r.Warnings {
		out.Warnings = append(out.Warnings, conv(f))
	}
	for dt, ps := range r.Ungoverned {
		for _, p := range ps {
			out.Ungoverned[dt] = append(out.Ungoverned[dt], schema.Display(p))
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
}

func writeTOON(w io.Writer, r *check.Result) {
	table := func(name string, fs []check.Fault) {
		fmt.Fprintf(w, "%s[%d]{file,kind,field,value,why}:\n", name, len(fs))
		for _, f := range fs {
			fmt.Fprintf(w, "  %s,%s,%s,%s,%s\n", toon(schema.Display(f.Path)), toon(f.Kind), toon(f.Field),
				toon(value.Truncate(shown(f.Value), 60)), toon(f.Why))
		}
	}
	table("faults", r.Faults)
	if len(r.Warnings) > 0 {
		table("warnings", r.Warnings)
	}
}

// toon quotes a TOON cell when it would otherwise read as a delimiter, a literal or
// structure.
func toon(s string) string {
	plain := s != "" && strings.TrimSpace(s) == s &&
		!strings.ContainsAny(s, ",:\"\\[]{}\n\r\t") && !strings.HasPrefix(s, "-") &&
		s != "true" && s != "false" && s != "null" && !looksNumeric(s)
	if plain {
		return s
	}
	b, _ := json.Marshal(s)
	return string(b)
}

func looksNumeric(s string) bool {
	for i, c := range s {
		if (c < '0' || c > '9') && c != '.' && !(i == 0 && (c == '-' || c == '+')) {
			return false
		}
	}
	return true
}

func runDocs(args []string, stdout, stderr io.Writer) int {
	fs, tax := newFlags("docs", `taxidermist docs [--check]

Generate <taxonomy>/format/: one page per doctype with a fill-in template, every
field's guidance and every vocabulary it uses, plus a README index. The index opens
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
