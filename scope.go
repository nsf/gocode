package main

import (
	"fmt"
	"strings"
	"go/ast"
)

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

//-------------------------------------------------------------------------
// Name foreignification
// Transforms name to a pair: nice name + real name. Used for modules
//-------------------------------------------------------------------------

func foreignifyName(name, realname string) string {
	return fmt.Sprint("$", name, "$", realname)
}

func isNameForeignified(name string) bool {
	return name[0] == '$'
}

func splitForeignName(name string) (string, string) {
	i := strings.Index(name[1:], "$")
	if i == -1 {
		panic("trying to split unforeignified name")
	}
	return name[1 : i+1], name[i+2:]
}

func filterForeignName(name string) string {
	if isNameForeignified(name) {
		_, b := splitForeignName(name)
		return b
	}
	return name
}

func foreignifyFuncFieldList(f *ast.FieldList, file *Scope) {
	if f == nil {
		return
	}

	for _, field := range f.List {
		foreignifyTypeExpr(field.Type, file)
	}
}

func foreignifyTypeExpr(e ast.Expr, file *Scope) {
	switch t := e.(type) {
	case *ast.StarExpr:
		foreignifyTypeExpr(t.X, file)
	case *ast.Ident:
		if !isNameForeignified(t.Name()) {
			decl := file.lookup(t.Name())
			if decl != nil && decl.Class == DECL_MODULE {
				realname := decl.Name
				t.Obj.Name = foreignifyName(t.Name(), realname)
			}
		}
	case *ast.ArrayType:
		foreignifyTypeExpr(t.Elt, file)
	case *ast.SelectorExpr:
		foreignifyTypeExpr(t.X, file)
	case *ast.FuncType:
		foreignifyFuncFieldList(t.Params, file)
		foreignifyFuncFieldList(t.Results, file)
	case *ast.MapType:
		foreignifyTypeExpr(t.Key, file)
		foreignifyTypeExpr(t.Value, file)
	case *ast.ChanType:
		foreignifyTypeExpr(t.Value, file)
	}
}
