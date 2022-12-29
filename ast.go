/*
 Copyright 2021 The GoPlus Authors (goplus.org)
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
     http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"log"
	"reflect"
	"strconv"
)

var (
	underscore = &ast.Ident{Name: "_"}
)

var (
	identTrue   = ident("true")
	identFalse  = ident("false")
	identNil    = ident("nil")
	identAppend = ident("append")
	identLen    = ident("len")
	identCap    = ident("cap")
	identNew    = ident("new")
	identMake   = ident("make")
	identIota   = ident("iota")
)

func ident(name string) *ast.Ident {
	return &ast.Ident{Name: name}
}

func boolean(v bool) *ast.Ident {
	if v {
		return identTrue
	}
	return identFalse
}

func toRecv(pkg *types.Package, recv *types.Var) *ast.FieldList {
	var names []*ast.Ident
	if name := recv.Name(); name != "" {
		names = []*ast.Ident{ident(name)}
	}
	fld := &ast.Field{Names: names, Type: toType(pkg, recv.Type())}
	return &ast.FieldList{List: []*ast.Field{fld}}
}

// -----------------------------------------------------------------------------
// function type

func toFieldList(pkg *types.Package, t *types.Tuple) []*ast.Field {
	if t == nil {
		return nil
	}
	n := t.Len()
	flds := make([]*ast.Field, n)
	for i := 0; i < n; i++ {
		item := t.At(i)
		var names []*ast.Ident
		if name := item.Name(); name != "" {
			names = []*ast.Ident{ident(name)}
		}
		typ := toType(pkg, item.Type())
		flds[i] = &ast.Field{Names: names, Type: typ}
	}
	return flds
}

func toFields(pkg *types.Package, t *types.Struct) []*ast.Field {
	n := t.NumFields()
	flds := make([]*ast.Field, n)
	for i := 0; i < n; i++ {
		item := t.Field(i)
		var names []*ast.Ident
		if !item.Embedded() {
			names = []*ast.Ident{{Name: item.Name()}}
		}
		typ := toType(pkg, item.Type())
		fld := &ast.Field{Names: names, Type: typ}
		if tag := t.Tag(i); tag != "" {
			fld.Tag = &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(tag)}
		}
		flds[i] = fld
	}
	return flds
}

func toVariadic(fld *ast.Field) {
	t, ok := fld.Type.(*ast.ArrayType)
	if !ok || t.Len != nil {
		panic("TODO: not a slice type")
	}
	fld.Type = &ast.Ellipsis{Elt: t.Elt}
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

// -----------------------------------------------------------------------------

func toType(pkg *types.Package, typ types.Type) ast.Expr {
	switch t := typ.(type) {
	case *types.Basic: // bool, int, etc
		return toBasicType(pkg, t)
	case *types.Pointer:
		return &ast.StarExpr{X: toType(pkg, t.Elem())}
	case *types.Named:
		return toNamedType(pkg, t)
	case *types.Interface:
		return toInterface(pkg, t)
	case *types.Slice:
		return toSliceType(pkg, t)
	case *types.Array:
		return toArrayType(pkg, t)
	case *types.Map:
		return toMapType(pkg, t)
	case *types.Struct:
		return toStructType(pkg, t)
	case *types.Chan:
		return toChanType(pkg, t)
	case *types.Signature:
		return toFuncType(pkg, t)
	}
	log.Panicln("TODO: toType -", reflect.TypeOf(typ))
	return nil
}

func toObjectExpr(pkg *types.Package, v types.Object) ast.Expr {
	vpkg, name := v.Pkg(), v.Name()
	if vpkg == nil || vpkg == g_daemon.autocomplete.typesPkg { // at universe or at this package
		return ident(name)
	}
	return &ast.SelectorExpr{
		X:   ident(vpkg.Name()),
		Sel: ident(name),
	}
}

func toBasicType(pkg *types.Package, t *types.Basic) ast.Expr {
	if t.Kind() == types.UnsafePointer {
		return &ast.SelectorExpr{X: ast.NewIdent("unsafe"), Sel: ast.NewIdent("Pointer")}
	}
	if (t.Info() & types.IsUntyped) != 0 {
		panic("unexpected: untyped type")
	}
	return &ast.Ident{Name: t.Name()}
}

func isUntyped(pkg *types.Package, typ types.Type) bool {
	switch t := typ.(type) {
	case *types.Basic:
		return (t.Info() & types.IsUntyped) != 0
	}
	return false
}

func toChanType(pkg *types.Package, t *types.Chan) ast.Expr {
	return &ast.ChanType{Value: toType(pkg, t.Elem()), Dir: chanDirs[t.Dir()]}
}

var (
	chanDirs = [...]ast.ChanDir{
		types.SendRecv: ast.SEND | ast.RECV,
		types.SendOnly: ast.SEND,
		types.RecvOnly: ast.RECV,
	}
)

func toStructType(pkg *types.Package, t *types.Struct) ast.Expr {
	list := toFields(pkg, t)
	return &ast.StructType{Fields: &ast.FieldList{List: list}}
}

func toArrayType(pkg *types.Package, t *types.Array) ast.Expr {
	var len ast.Expr
	if n := t.Len(); n < 0 {
		len = &ast.Ellipsis{}
	} else {
		len = &ast.BasicLit{Kind: token.INT, Value: strconv.FormatInt(t.Len(), 10)}
	}
	return &ast.ArrayType{Len: len, Elt: toType(pkg, t.Elem())}
}

func toSliceType(pkg *types.Package, t *types.Slice) ast.Expr {
	return &ast.ArrayType{Elt: toType(pkg, t.Elem())}
}

func toMapType(pkg *types.Package, t *types.Map) ast.Expr {
	return &ast.MapType{Key: toType(pkg, t.Key()), Value: toType(pkg, t.Elem())}
}

func toInterface(pkg *types.Package, t *types.Interface) ast.Expr {
	var flds []*ast.Field
	for i, n := 0, t.NumEmbeddeds(); i < n; i++ {
		typ := toType(pkg, t.EmbeddedType(i))
		fld := &ast.Field{Type: typ}
		flds = append(flds, fld)
	}
	for i, n := 0, t.NumExplicitMethods(); i < n; i++ {
		fn := t.ExplicitMethod(i)
		name := ident(fn.Name())
		typ := toFuncType(pkg, fn.Type().(*types.Signature))
		fld := &ast.Field{Names: []*ast.Ident{name}, Type: typ}
		flds = append(flds, fld)
	}
	return &ast.InterfaceType{Methods: &ast.FieldList{List: flds}}
}
