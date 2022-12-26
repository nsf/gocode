//go:build go1.18
// +build go1.18

package main

import (
	"go/ast"
	"go/token"
)

// converts type expressions like:
// ast.Expr
// *ast.Expr
// $ast$go/ast.Expr
// to a path that can be used to lookup a type related Decl
func get_type_path(e ast.Expr) (r type_path) {
	if e == nil {
		return type_path{"", "", nil}
	}

	switch t := e.(type) {
	case *ast.Ident:
		r.name = t.Name
	case *ast.StarExpr:
		r = get_type_path(t.X)
	case *ast.SelectorExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			r.pkg = ident.Name
		}
		r.name = t.Sel.Name
	case *ast.IndexExpr:
		r = get_type_path(t.X)
		r.indices = []ast.Expr{t.Index}
	case *ast.IndexListExpr:
		r = get_type_path(t.X)
		r.indices = t.Indices
	}
	return
}

func ast_decl_typeparams(decl ast.Decl) *ast.FieldList {
	switch t := decl.(type) {
	case *ast.GenDecl:
		if t.Tok == token.TYPE {
			if len(t.Specs) > 0 {
				if spec, ok := t.Specs[0].(*ast.TypeSpec); ok {
					return spec.TypeParams
				}
			}
		}
	case *ast.FuncDecl:
		return t.Type.TypeParams
	}
	return nil
}
