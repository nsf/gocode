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

type TypeParam = types.TypeParam
type TypeParamList = types.TypeParamList

var TILDE = token.TILDE

func newFuncType(tparams, params, results *ast.FieldList) *ast.FuncType {
	return &ast.FuncType{TypeParams: tparams, Params: params, Results: results}
}

func newTypeSpec(name string, tparams *ast.FieldList) *ast.TypeSpec {
	return &ast.TypeSpec{
		Name:       ast.NewIdent(name),
		TypeParams: tparams,
	}
}

func toTypeParam(pkg *types.Package, t *TypeParam) ast.Expr {
	return toObjectExpr(pkg, t.Obj())
}

func ForSignature(sig *types.Signature) *TypeParamList {
	return sig.TypeParams()
}

func ForFuncType(typ *ast.FuncType) *ast.FieldList {
	return typ.TypeParams
}

// RecvTypeParams returns a nil slice.
func RecvTypeParams(sig *types.Signature) *TypeParamList {
	return sig.RecvTypeParams()
}

func ForNamed(named *types.Named) *TypeParamList {
	return named.TypeParams()
}

func toFieldListX(pkg *types.Package, t *types.TypeParamList) *ast.FieldList {
	if t == nil {
		return nil
	}
	n := t.Len()
	flds := make([]*ast.Field, n)
	for i := 0; i < n; i++ {
		item := t.At(i)
		names := []*ast.Ident{ast.NewIdent(item.Obj().Name())}
		typ := toType(pkg, item.Constraint())
		flds[i] = &ast.Field{Names: names, Type: typ}
	}
	return &ast.FieldList{
		List: flds,
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
		TypeParams: toFieldListX(pkg, sig.TypeParams()),
		Params:     &ast.FieldList{List: params},
		Results:    &ast.FieldList{List: results},
	}
}

func toTypeSpec(pkg *types.Package, t *types.TypeName) *ast.TypeSpec {
	var assign token.Pos
	if t.IsAlias() {
		assign = 1
	}
	typ := t.Type()
	ts := &ast.TypeSpec{
		Name:   ast.NewIdent(t.Name()),
		Assign: assign,
		Type:   toType(pkg, typ.Underlying()),
	}
	if named, ok := typ.(*types.Named); ok {
		ts.TypeParams = toFieldListX(pkg, named.TypeParams())
	}
	return ts
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

func lookup_types_instance_sig(text string, info *types.Info) types.Type {
	for _, v := range info.Instances {
		if sig, ok := v.Type.(*types.Signature); ok {
			for i := 0; i < sig.Results().Len(); i++ {
				typ := sig.Results().At(i).Type()
				if star, ok := typ.(*types.Pointer); ok {
					typ = star.Elem()
				}
				expr := toType(nil, typ)
				if types.ExprString(expr) == text {
					return typ
				}
			}
		}
	}
	return nil
}
