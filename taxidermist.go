// Package taxidermist checks markdown frontmatter against a taxonomy directory.
//
// It is the library behind the taxidermist command. `taxidermist check --json`
// prints the Result that Validate and ValidateContent return, so a program in any
// language gets the same findings from the binary that a Go program gets here.
package taxidermist

import (
	"fmt"

	"github.com/jvanmelckebeke/taxidermist/internal/check"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

// Kinds lists the fault kinds a Finding in Result.Faults can carry. A warning's kind
// is always "mismatch".
var Kinds = check.Kinds

// Taxonomy is a loaded taxonomy directory. Load it once and validate any number of
// files against it.
type Taxonomy struct {
	s *schema.Schema
}

// Load reads a taxonomy directory: scope.yaml, the types/ files and the rest. An
// error means the schema itself is broken and nothing can be checked against it.
func Load(dir string) (*Taxonomy, error) {
	s, err := schema.Load(dir)
	if err != nil {
		return nil, err
	}
	return &Taxonomy{s: s}, nil
}

// Validate checks files on disk the way `taxidermist check FILE...` does. With no
// paths it checks every governed root. A path that is not markdown, does not exist,
// or sits outside the roots scope.yaml governs is skipped, so a caller can pass any
// list of files and scope.yaml decides which ones count.
func (t *Taxonomy) Validate(paths ...string) (*Result, error) {
	files, err := check.Files(t.s, paths)
	if err != nil {
		return nil, err
	}
	return newResult(check.New(t.s).Run(files)), nil
}

// ValidateContent checks content as if it were the markdown file at path, which
// need not exist yet. The path still matters: it decides the folder rules, the
// exemptions and the slug a page must not share. A path outside the governed roots
// is an error rather than a skip, since the caller named it on purpose.
func (t *Taxonomy) ValidateContent(path string, content []byte) (*Result, error) {
	abs, ok, err := check.Governed(t.s, path)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("taxidermist: %s is not a markdown file under a root scope.yaml governs", path)
	}
	return newResult(check.New(t.s).Content(abs, content)), nil
}

// Result is what one validation found. Its JSON form is the output of
// `taxidermist check --json`.
type Result struct {
	// Governed counts the files checked whose doctype has a schema.
	Governed int `json:"governed"`
	// Faults fail a check. They come in file order.
	Faults []Finding `json:"faults"`
	// Warnings never fail a check.
	Warnings []Finding `json:"warnings"`
	// Ungoverned maps a doctype that has no types/ file to the files that carry it,
	// when scope.yaml says `ungoverned: report`.
	Ungoverned map[string][]string `json:"ungoverned"`
}

// OK is true when there are no faults.
func (r *Result) OK() bool { return len(r.Faults) == 0 }

// Finding is one fault or warning.
type Finding struct {
	// File is the path relative to the working directory when it sits under it,
	// otherwise absolute.
	File string `json:"file"`
	// Kind is the rule the file broke: one of Kinds, or "mismatch" for a warning.
	Kind string `json:"kind"`
	// Field is the frontmatter key, or a placeholder in angle brackets such as
	// <frontmatter> or <slug> when the fault is not about one key.
	Field string `json:"field"`
	// Value is the offending value as YAML text, or nil when there is none, as for
	// a missing field.
	Value *string `json:"value"`
	// Why says what is wrong and what the schema allows.
	Why string `json:"why"`
}

func newResult(r *check.Result) *Result {
	out := &Result{Governed: r.Governed(), Faults: []Finding{}, Warnings: []Finding{}, Ungoverned: map[string][]string{}}
	for _, f := range r.Faults {
		out.Faults = append(out.Faults, newFinding(f))
	}
	for _, f := range r.Warnings {
		out.Warnings = append(out.Warnings, newFinding(f))
	}
	for dt, ps := range r.Ungoverned {
		for _, p := range ps {
			out.Ungoverned[dt] = append(out.Ungoverned[dt], schema.Display(p))
		}
	}
	return out
}

func newFinding(f check.Fault) Finding {
	out := Finding{File: schema.Display(f.Path), Kind: f.Kind, Field: f.Field, Why: f.Why}
	if f.Value != nil {
		v := value.Text(f.Value)
		out.Value = &v
	}
	return out
}
