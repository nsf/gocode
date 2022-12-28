//go:build !go1.18
// +build !go1.18

package main

import (
	"go/ast"
	"go/token"
	"go/types"

	pkgwalk "github.com/visualfc/gotools/types"
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
	}
	return
}

func ast_decl_typeparams(decl ast.Decl) *ast.FieldList {
	return nil
}

func hasTypeParams(typ types.Type) bool {
	return false
}

func toNamedType(pkg *types.Package, t *types.Named) ast.Expr {
	return toObjectExpr(pkg, t.Obj())
}

func lookup_types_near_instance(ident *ast.Ident, pos token.Pos, info *types.Info) types.Type {
	return nil
}

func DefaultPkgConfig() *pkgwalk.PkgConfig {
	conf := &pkgwalk.PkgConfig{IgnoreFuncBodies: false, AllowBinary: true, WithTestFiles: true}
	conf.Info = &types.Info{
		Uses:       make(map[*ast.Ident]types.Object),
		Defs:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Scopes:     make(map[ast.Node]*types.Scope),
		Implicits:  make(map[ast.Node]types.Object),
	}
	conf.XInfo = &types.Info{
		Uses:       make(map[*ast.Ident]types.Object),
		Defs:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Scopes:     make(map[ast.Node]*types.Scope),
		Implicits:  make(map[ast.Node]types.Object),
	}
	return conf
}
