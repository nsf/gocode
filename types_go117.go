//go:build !go1.18
// +build !go1.18

package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"strings"

	pkgwalk "github.com/visualfc/gotools/types"
)

func unsupported() {
	panic("type parameters are unsupported at this go version")
}

var TILDE = token.VAR + 3

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

func ForFuncType(typ *ast.FuncType) *ast.FieldList {
	return nil
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

func lookup_types_instance_sig(text string, info *types.Info) types.Type {
	return nil
}

func pretty_print_type_expr(out io.Writer, e ast.Expr, canonical_aliases map[string]string) {
	switch t := e.(type) {
	case *ast.StarExpr:
		fmt.Fprintf(out, "*")
		pretty_print_type_expr(out, t.X, canonical_aliases)
	case *ast.Ident:
		if strings.HasPrefix(t.Name, "$") {
			// beautify anonymous types
			switch t.Name[1] {
			case 's':
				fmt.Fprintf(out, "struct")
			case 'i':
				// ok, in most cases anonymous interface is an
				// empty interface, I'll just pretend that
				// it's always true
				fmt.Fprintf(out, "interface{}")
			}
		} else if !*g_debug && strings.HasPrefix(t.Name, "!") {
			// these are full package names for disambiguating and pretty
			// printing packages within packages, e.g.
			// !go/ast!ast vs. !github.com/nsf/my/ast!ast
			// another ugly hack, if people are punished in hell for ugly hacks
			// I'm screwed...
			emarkIdx := strings.LastIndex(t.Name, "!")
			path := t.Name[1:emarkIdx]
			alias := canonical_aliases[path]
			if alias == "" {
				alias = t.Name[emarkIdx+1:]
			}
			fmt.Fprintf(out, alias)
		} else {
			fmt.Fprintf(out, t.Name)
		}
	case *ast.ArrayType:
		al := ""
		if t.Len != nil {
			al = get_array_len(t.Len)
		}
		if al != "" {
			fmt.Fprintf(out, "[%s]", al)
		} else {
			fmt.Fprintf(out, "[]")
		}
		pretty_print_type_expr(out, t.Elt, canonical_aliases)
	case *ast.SelectorExpr:
		pretty_print_type_expr(out, t.X, canonical_aliases)
		fmt.Fprintf(out, ".%s", t.Sel.Name)
	case *ast.FuncType:
		fmt.Fprintf(out, "func(")
		pretty_print_func_field_list(out, t.Params, canonical_aliases)
		fmt.Fprintf(out, ")")

		buf := bytes.NewBuffer(make([]byte, 0, 256))
		nresults := pretty_print_func_field_list(buf, t.Results, canonical_aliases)
		if nresults > 0 {
			results := buf.String()
			if strings.IndexAny(results, ", ") != -1 {
				results = "(" + results + ")"
			}
			fmt.Fprintf(out, " %s", results)
		}
	case *ast.MapType:
		fmt.Fprintf(out, "map[")
		pretty_print_type_expr(out, t.Key, canonical_aliases)
		fmt.Fprintf(out, "]")
		pretty_print_type_expr(out, t.Value, canonical_aliases)
	case *ast.InterfaceType:
		fmt.Fprintf(out, "interface{}")
	case *ast.Ellipsis:
		fmt.Fprintf(out, "...")
		pretty_print_type_expr(out, t.Elt, canonical_aliases)
	case *ast.StructType:
		fmt.Fprintf(out, "struct")
	case *ast.ChanType:
		switch t.Dir {
		case ast.RECV:
			fmt.Fprintf(out, "<-chan ")
		case ast.SEND:
			fmt.Fprintf(out, "chan<- ")
		case ast.SEND | ast.RECV:
			fmt.Fprintf(out, "chan ")
		}
		pretty_print_type_expr(out, t.Value, canonical_aliases)
	case *ast.ParenExpr:
		fmt.Fprintf(out, "(")
		pretty_print_type_expr(out, t.X, canonical_aliases)
		fmt.Fprintf(out, ")")
	case *ast.BadExpr:
		// TODO: probably I should check that in a separate function
		// and simply discard declarations with BadExpr as a part of their
		// type
	default:
		// the element has some weird type, just ignore it
	}
}
