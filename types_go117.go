//go:build !go1.18
// +build !go1.18

package main

import (
	"go/ast"
	"go/token"
	"go/types"

	pkgwalk "github.com/visualfc/gotools/types"
)

func unsupported() {
	panic("type parameters are unsupported at this go version")
}

type TypeParam struct{ types.Type }

func (*TypeParam) String() string           { unsupported(); return "" }
func (*TypeParam) Underlying() types.Type   { unsupported(); return nil }
func (*TypeParam) Index() int               { unsupported(); return 0 }
func (*TypeParam) Constraint() types.Type   { unsupported(); return nil }
func (*TypeParam) SetConstraint(types.Type) { unsupported() }
func (*TypeParam) Obj() *types.TypeName     { unsupported(); return nil }

// TypeParamList is a placeholder for an empty type parameter list.
type TypeParamList struct{}

func (*TypeParamList) Len() int          { return 0 }
func (*TypeParamList) At(int) *TypeParam { unsupported(); return nil }

func newFuncType(tparams, params, results *ast.FieldList) *ast.FuncType {
	return &ast.FuncType{Params: params, Results: results}
}

func newTypeSpec(name string, tparams *ast.FieldList) *ast.TypeSpec {
	return &ast.TypeSpec{
		Name: ast.NewIdent(name),
	}
}

func toTypeParam(pkg *types.Package, t *TypeParam) ast.Expr {
	unsupported()
	return nil
}

func toTypeSpec(pkg *types.Package, t *types.TypeName) *ast.TypeSpec {
	var assign token.Pos
	if t.IsAlias() {
		assign = 1
	}
	typ := t.Type()
	return &ast.TypeSpec{
		Name:   ast.NewIdent(t.Name()),
		Assign: assign,
		Type:   toType(pkg, typ.Underlying()),
	}
}

func toFuncType(pkg *types.Package, sig *types.Signature) *ast.FuncType {
	params := toFieldList(pkg, sig.Params())
	results := toFieldList(pkg, sig.Results())
	if sig.Variadic() {
		n := len(params)
		if n == 0 {
			panic("TODO: toFuncType error")
		}
		toVariadic(params[n-1])
	}
	return &ast.FuncType{
		Params:  &ast.FieldList{List: params},
		Results: &ast.FieldList{List: results},
	}
}

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

func funcHasTypeParams(typ *ast.FuncType) bool {
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
