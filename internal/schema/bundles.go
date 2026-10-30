package schema

// Trait is a field bundle a doctype composes.
type Trait struct {
	Name   string
	Fields []*Field
}

func (s *Schema) Trait(name string) *Trait {
	for _, t := range s.Traits {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// loadTraits reads traits.yaml: field bundles a doctype composes.
func (s *Schema) loadTraits() error {
	var traits struct {
		Traits Ordered[Ordered[*Field]] `yaml:"traits"`
	}
	if err := read(s.Dir, "traits.yaml", &traits); err != nil {
		return err
	}
	for i, name := range traits.Traits.Keys {
		t := &Trait{Name: name}
		fs := traits.Traits.Vals[i]
		for j, fname := range fs.Keys {
			f := fs.Vals[j]
			f.Name = fname
			if err := s.bind(f, "traits.yaml"); err != nil {
				return err
			}
			t.Fields = append(t.Fields, f)
		}
		s.Traits = append(s.Traits, t)
	}
	return nil
}

// loadSingletons reads singletons.yaml: shared fields that form no bundle.
func (s *Schema) loadSingletons() error {
	var singles struct {
		Fields Ordered[*Field] `yaml:"fields"`
	}
	if err := read(s.Dir, "singletons.yaml", &singles); err != nil {
		return err
	}
	for i, name := range singles.Fields.Keys {
		f := singles.Fields.Vals[i]
		f.Name = name
		if err := s.bind(f, "singletons.yaml"); err != nil {
			return err
		}
		s.Singletons = append(s.Singletons, f)
	}
	return nil
}
