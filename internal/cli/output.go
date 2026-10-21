package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/check"
	"github.com/jvanmelckebeke/taxidermist/internal/schema"
	"github.com/jvanmelckebeke/taxidermist/internal/value"
)

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
