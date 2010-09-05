package main

type Scope struct {
	parent   *Scope // nil for universe scope
	entities map[string]*Decl
}

func NewScope(outer *Scope) *Scope {
	s := new(Scope)
	s.parent = outer
	s.entities = make(map[string]*Decl)
	return s
}

func AdvanceScope(s *Scope) *Scope {
	if len(s.entities) == 0 {
		return s
	}
	return NewScope(s)
}

// adds declaration or returns an existing one
func (s *Scope) addNamedDecl(d *Decl) *Decl {
	return s.addDecl(d.Name, d)
}

func (s *Scope) addDecl(name string, d *Decl) *Decl {
	decl, ok := s.entities[name]
	if !ok {
		s.entities[name] = d
		return d
	}
	return decl
}

func (s *Scope) replaceDecl(name string, d *Decl) {
	s.entities[name] = d
}

func (s *Scope) mergeDecl(d *Decl) {
	decl, ok := s.entities[d.Name]
	if !ok {
		s.entities[d.Name] = d
	} else {
		decl := decl.DeepCopy()
		decl.ExpandOrReplace(d)
		s.entities[d.Name] = decl
	}
}

func (s *Scope) lookup(name string) *Decl {
	decl, ok := s.entities[name]
	if !ok {
		if s.parent != nil {
			return s.parent.lookup(name)
		} else {
			return nil
		}
	}
	return decl
}
