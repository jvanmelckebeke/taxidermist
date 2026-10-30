package docs

import (
	"sort"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

// vocabLink links a vocabulary's own page from a page in format/.
func vocabLink(name string) string {
	return "[`" + name + "`](vocabularies/" + name + ".md)"
}

// valuesTable is the table of a field's or a vocabulary's values and what each means.
func valuesTable(enum []schema.Def, groups bool) []string {
	grouped, nots := false, false
	for _, d := range enum {
		grouped = grouped || (groups && d.Group != "")
		nots = nots || d.Not != ""
	}
	head, rule := "| value |", "|---|"
	if grouped {
		head, rule = head+" group |", rule+"---|"
	}
	head, rule = head+" means |", rule+"---|"
	if nots {
		head, rule = head+" not for |", rule+"---|"
	}
	L := []string{head, rule}
	seen := map[string]bool{}
	var keys []string
	for _, d := range enum {
		if !seen[d.Key] {
			seen[d.Key] = true
			keys = append(keys, d.Key)
		}
	}
	sort.Strings(keys)
	for _, x := range keys {
		d := lookup(enum, x)
		row := "| `" + x + "` |"
		if grouped {
			row += " " + d.Group + " |"
		}
		row += " " + cell(d.Means) + " |"
		if nots {
			row += " " + cell(d.Not) + " |"
		}
		L = append(L, row)
	}
	return L
}

func hasMeanings(enum []schema.Def) bool {
	for _, d := range enum {
		if d.Means != "" {
			return true
		}
	}
	return false
}

func lookup(enum []schema.Def, key string) schema.Def {
	for _, d := range enum {
		if d.Key == key {
			return d
		}
	}
	return schema.Def{}
}

// cell keeps a definition inside its table cell.
func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ")
}

// VocabularyDir is the folder under format/ that holds one page per vocabulary.
const VocabularyDir = "vocabularies"

// vocabularyPage is one vocabulary's values and what each means, with the file that
// declares them.
func vocabularyPage(s *schema.Schema, name string) string {
	L := []string{"# Vocabulary `" + name + "`", "", banner(s), ""}
	if src, ok := s.Sources[name]; ok {
		line := "Read from [`" + src.From + "`](../../../" + src.From + "): "
		if src.At != "" {
			line += "the keys of `" + src.At + "`"
		} else {
			line += "its top-level keys"
		}
		if src.Means != "" {
			line += ", each meaning its entry's `" + src.Means + ":`"
		}
		L = append(L, line+". A value is added there, not here.", "")
	} else {
		L = append(L, "Defined in [../../vocabularies/"+name+".yaml](../../vocabularies/"+name+".yaml).", "")
	}
	enum := s.Vocabularies[name]
	if hasMeanings(enum) {
		L = append(L, valuesTable(enum, true)...)
	} else {
		var keys []string
		for _, d := range enum {
			keys = append(keys, "`"+d.Key+"`")
		}
		sort.Strings(keys)
		L = append(L, "Values: "+strings.Join(keys, " · ")+".")
	}
	return strings.Join(L, "\n")
}
