package docs

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jvanmelckebeke/taxidermist/internal/schema"
)

// indexPage is the README of format/: every page, the folder rules and the rules that
// hold on every page, led by the hand-written house rules.
func indexPage(s *schema.Schema) (string, error) {
	idx := []string{
		"# Writing frontmatter, one page per doctype", "", banner(s), "",
		"Each page resolves one `type:`: its obligations, a fill-in template, what to put in " +
			"every field and every vocabulary it uses. Generated from " + readmeLink(s) +
			", which is the schema and the thing to edit.", "",
	}
	var files []string
	for _, dt := range s.Doctypes() {
		if t := s.Types[dt]; t.IsYAML() {
			files = append(files, "- [`"+t.Files+"`]("+t.Slug+".md)")
		} else {
			idx = append(idx, "- [`"+dt+"`]("+t.Slug+".md)")
		}
	}
	if len(files) > 0 {
		idx = append(append(idx, "", "## YAML files", "",
			"A YAML file under a governed root takes the doctype whose `files:` glob matches its path. "+
				"Each page resolves one glob."), "")
		idx = append(idx, files...)
	}
	if names := s.VocabularyNames(); len(names) > 0 {
		idx = append(idx, "", "## Vocabularies", "", "Every shared value list, on a page of its own.", "")
		for _, v := range names {
			idx = append(idx, "- "+vocabLink(v))
		}
	}
	if len(s.Segments) > 0 {
		idx = append(idx, "", "## Folders", "",
			"A folder name directly under these directories, or the filename of a page sitting directly "+
				"there, must be an allowed value. `index.md` and `README.md` there describe the directory itself.", "")
		for _, sg := range s.Segments {
			line := "- `" + sg.Dir + "/<name>`: "
			if sg.Pattern != nil {
				line += "`<name>` matches `" + sg.Pattern.String() + "`."
			} else {
				line += "`<name>` is a value of " + vocabLink(sg.Vocabulary) + "."
			}
			if sg.Field != "" {
				line += " A file whose `" + sg.Field + ":` names a different value gets a warning."
			}
			idx = append(idx, line)
		}
	}
	idx = append(idx, "", "## What holds on every page", "")
	house, err := os.ReadFile(filepath.Join(s.Dir, HouseRules))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if h := strings.TrimSpace(string(house)); h != "" {
		idx = append(idx, h, "")
	}
	idx = append(idx,
		"**A field's `definitions` keys are its allowed values.** There is no second list, so a "+
			"value cannot exist until someone writes down what it means. Prefer an existing one, "+
			"because a near-synonym fragments every filter. If nothing fits, add it to the right file "+
			"under "+readmeLink(s)+" with its one line in the same change, then run `"+s.Command("docs")+"`. "+
			"`"+s.Command("check")+"` fails otherwise.", "",
		"**YAML frontmatter is hand-written and fails silently.** A block that opens and does not "+
			"parse leaves the file invisible to every tool that reads it, while it looks correct on "+
			"disk. An unescaped quote, or a `: ` inside an unquoted scalar, is where it usually breaks.", "")
	return strings.Join(idx, "\n"), nil
}
