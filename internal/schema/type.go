package schema

import (
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Condition makes a field required when every listed field has the listed value.
type Condition struct {
	Field string
	When  Ordered[any]
}

// Type is one doctype.
type Type struct {
	Doctype      string            `yaml:"type"` // the literal `type:` value; defaults to the slug
	Slug         string            `yaml:"-"`    // the file stem, used for the format page name
	Traits       []string          `yaml:"traits"`
	Required     []string          `yaml:"required"`
	Optional     []string          `yaml:"optional"`
	RequiredWhen []Condition       `yaml:"-"`
	Directory    string            `yaml:"directory"` // where its pages live; `{field}` fills from the page
	Files        string            `yaml:"files"`     // a path glob naming the YAML files of this doctype
	Shape        string            `yaml:"shape"`     // a YAML file's top level: fields, map or list
	Key          *Field            `yaml:"key"`       // `shape: map` only: what each top-level key must be
	Fields       Ordered[*Field]   `yaml:"fields"`
	Resolved     map[string]*Field `yaml:"-"` // own fields, then singletons, then traits
}

func (t *Type) UnmarshalYAML(n *yaml.Node) error {
	if err := strict(n, "a type file", "type", "traits", "required", "optional", "required_when", "directory", "files", "shape", "key", "fields"); err != nil {
		return err
	}
	type plain Type
	var raw struct {
		plain        `yaml:",inline"`
		RequiredWhen Ordered[Ordered[any]] `yaml:"required_when"`
	}
	if err := n.Decode(&raw); err != nil {
		return err
	}
	*t = Type(raw.plain)
	for i, f := range raw.RequiredWhen.Keys {
		t.RequiredWhen = append(t.RequiredWhen, Condition{Field: f, When: raw.RequiredWhen.Vals[i]})
	}
	return nil
}

// Shapes are the values `shape:` takes. `fields` is the default.
var Shapes = []string{"fields", "map", "list"}

// IsYAML is true for a doctype that governs plain YAML files rather than frontmatter.
func (t *Type) IsYAML() bool { return t.Files != "" }

// Declared is every field the doctype may carry.
func (t *Type) Declared() map[string]bool {
	out := map[string]bool{}
	for _, n := range t.Required {
		out[n] = true
	}
	for _, n := range t.Optional {
		out[n] = true
	}
	return out
}

// loadTypes reads every types/<slug>.yaml.
func (s *Schema) loadTypes() error {
	files, _ := filepath.Glob(filepath.Join(s.Dir, "types", "*.yaml"))
	sort.Strings(files)
	for _, file := range files {
		if err := s.loadType(file); err != nil {
			return err
		}
	}
	return nil
}

func (s *Schema) loadType(file string) error {
	slug := strings.TrimSuffix(filepath.Base(file), ".yaml")
	rel := "types/" + filepath.Base(file)
	t := &Type{}
	if err := read(s.Dir, rel, t); err != nil {
		return err
	}
	t.Slug = slug
	if t.Doctype == "" {
		t.Doctype = slug
	}
	t.Resolved = map[string]*Field{}
	if other, dup := s.Types[t.Doctype]; dup {
		return errf("types/%s.yaml and %s both declare `type: %s`.", other.Slug, rel, t.Doctype)
	}
	if err := t.checkDirectory(rel); err != nil {
		return err
	}
	if err := s.checkFiles(t, rel); err != nil {
		return err
	}
	for i, name := range t.Fields.Keys {
		f := t.Fields.Vals[i]
		f.Name = name
		if err := s.bind(f, rel); err != nil {
			return err
		}
	}

	if err := s.compose(t, rel); err != nil {
		return err
	}
	s.Types[t.Doctype] = t
	return nil
}

// compose resolves every field the doctype carries: its traits' fields, then the
// singletons that apply to it, then its own, each later one winning.
func (s *Schema) compose(t *Type, rel string) error {
	declared := t.Declared()
	for _, name := range t.Traits {
		tr := s.Trait(name)
		if tr == nil {
			var known []string
			for _, x := range s.Traits {
				known = append(known, x.Name)
			}
			return errf("%s composes unknown trait `%s`. traits.yaml has %s.", rel, name, strings.Join(known, ", "))
		}
		var missing []string
		for _, f := range tr.Fields {
			if !declared[f.Name] {
				missing = append(missing, f.Name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return errf("%s composes trait `%s` but never allows %s. List them in required or optional, "+
				"or drop the trait: a trait list that omits its own fields cannot be trusted to mean what it says.",
				rel, name, strings.Join(missing, ", "))
		}
		for _, f := range tr.Fields {
			t.Resolved[f.Name] = f
			s.own(f.Name, t.Doctype)
		}
	}
	for _, f := range s.Singletons {
		for _, d := range f.AppliesTo {
			if d == t.Doctype {
				t.Resolved[f.Name] = f
				s.own(f.Name, t.Doctype)
			}
		}
	}
	for _, f := range t.Fields.Vals {
		t.Resolved[f.Name] = f
		s.own(f.Name, t.Doctype)
	}
	return nil
}

// Placeholder is a `{field}` in a type's `directory:`.
var Placeholder = regexp.MustCompile(`\{(\w+)\}`)

// DirectoryPrefix is the part of a `directory:` pattern before its first placeholder,
// the folder under which a page's slug must be unique. Empty when the pattern opens
// with a placeholder.
func DirectoryPrefix(pattern string) string {
	var fixed []string
	for _, part := range strings.Split(pattern, "/") {
		if Placeholder.MatchString(part) {
			break
		}
		fixed = append(fixed, part)
	}
	return strings.Join(fixed, "/")
}

// checkDirectory rejects a `directory:` placeholder naming a field the doctype never
// carries, which would otherwise drop out of the path and check nothing.
func (t *Type) checkDirectory(rel string) error {
	if t.Directory == "" {
		return nil
	}
	if strings.HasPrefix(t.Directory, "/") || strings.ContainsAny(t.Directory, "*?[") {
		return errf("%s: `directory: %s` must be a relative path with no wildcard.", rel, t.Directory)
	}
	declared := t.Declared()
	for _, c := range t.RequiredWhen {
		declared[c.Field] = true
	}
	for _, m := range Placeholder.FindAllStringSubmatch(t.Directory, -1) {
		if !declared[m[1]] {
			return errf("%s: `directory: %s` fills `{%s}` from a field the doctype does not declare.", rel, t.Directory, m[1])
		}
	}
	return nil
}

// checkFiles validates the keys that only a YAML doctype takes.
func (s *Schema) checkFiles(t *Type, rel string) error {
	if !t.IsYAML() {
		if t.Shape != "" || t.Key != nil {
			return errf("%s: `shape:` and `key:` go with `files:`; a markdown doctype's shape is its frontmatter.", rel)
		}
		return nil
	}
	if strings.HasPrefix(t.Files, "/") || slices.Contains(strings.Split(t.Files, "/"), "..") {
		return errf("%s: `files: %s` must be a path glob relative to %s, inside it.", rel, t.Files, Display(s.Base))
	}
	if t.Directory != "" {
		return errf("%s: `directory:` places markdown pages; a YAML doctype's place is its `files:` glob.", rel)
	}
	if t.Shape == "" {
		t.Shape = "fields"
	}
	if !slices.Contains(Shapes, t.Shape) {
		return errf("%s: `shape: %s`; it takes %s.", rel, t.Shape, strings.Join(Shapes, ", "))
	}
	if t.Key != nil {
		if t.Shape != "map" {
			return errf("%s: `key:` describes the entry names of `shape: map`, and this type is `shape: %s`.", rel, t.Shape)
		}
		t.Key.Name = "key"
		if err := s.bind(t.Key, rel); err != nil {
			return err
		}
	}
	return nil
}

func (s *Schema) own(field, doctype string) {
	if s.Owners[field] == nil {
		s.Owners[field] = map[string]bool{}
	}
	s.Owners[field][doctype] = true
}
