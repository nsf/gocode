//go:build go1.18
// +build go1.18

package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"

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
			if r.pkg == "main" {
				r.pkg = ""
			}
		}
		r.name = t.Sel.Name
	case *ast.IndexExpr:
		r = get_type_path(t.X)
		r.targs = []ast.Expr{t.Index}
	case *ast.IndexListExpr:
		r = get_type_path(t.X)
		r.targs = t.Indices
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

func hasTypeParams(typ types.Type) bool {
	switch t := typ.(type) {
	case *types.Named:
		return t.TypeParams() != nil && (t.Origin() == t)
	case *types.Signature:
		return t.TypeParams() != nil
	}
	return false
}

func funcHasTypeParams(typ *ast.FuncType) bool {
	return typ.TypeParams != nil
}

func toNamedType(pkg *types.Package, t *types.Named) ast.Expr {
	expr := toObjectExpr(pkg, t.Obj())
	if targs := t.TypeArgs(); targs != nil {
		n := targs.Len()
		indices := make([]ast.Expr, n)
		for i := 0; i < n; i++ {
			indices[i] = toType(pkg, targs.At(i))
		}
		if n == 1 {
			expr = &ast.IndexExpr{
				X:     expr,
				Index: indices[0],
			}
		} else {
			expr = &ast.IndexListExpr{
				X:       expr,
				Indices: indices,
			}
		}
	}
	return expr
}

func lookup_types_near_instance(ident *ast.Ident, pos token.Pos, info *types.Info) types.Type {
	var ar []*typ_distance
	for k, v := range info.Instances {
		if ident.Name == k.Name && pos > k.End() {
			ar = append(ar, &typ_distance{pos - k.End(), v.Type})
		}
	}
	switch len(ar) {
	case 0:
		return nil
	case 1:
		return ar[0].typ
	default:
		sort.Slice(ar, func(i, j int) bool {
			return ar[i].pos < ar[j].pos
		})
		return ar[0].typ
	}
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
		Instances:  make(map[*ast.Ident]types.Instance),
	}
	conf.XInfo = &types.Info{
		Uses:       make(map[*ast.Ident]types.Object),
		Defs:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Scopes:     make(map[ast.Node]*types.Scope),
		Implicits:  make(map[ast.Node]types.Object),
		Instances:  make(map[*ast.Ident]types.Instance),
	}
	return conf
}
