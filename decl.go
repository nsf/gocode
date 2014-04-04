package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"reflect"
	"strings"
	"sync"
)

// decl.class
type declClass int16

const (
	declInvalid = declClass(-1 + iota)

	// these are in a sorted order
	declConst
	declFunc
	declPackage
	declType
	declVar

	// this one serves as a temporary type for those methods that were
	// declared before their actual owner
	declMethodsStub
)

func (this declClass) String() string {
	switch this {
	case declInvalid:
		return "PANIC"
	case declConst:
		return "const"
	case declFunc:
		return "func"
	case declPackage:
		return "package"
	case declType:
		return "type"
	case declVar:
		return "var"
	case declMethodsStub:
		return "IF YOU SEE THIS, REPORT A BUG" // :D
	}
	panic("unreachable")
}

// decl.flags
type declFlags int16

const (
	declForeign = declFlags(1 << iota) // imported from another package

	// means that the decl is a part of the range statement
	// its type is inferred in a special way
	declRangevar

	// for preventing infinite recursions and loops in type inference code
	declVisited
)

//-------------------------------------------------------------------------
// decl
//
// The most important data structure of the whole gocode project. It
// describes a single declaration and its children.
//-------------------------------------------------------------------------

type decl struct {
	// Name starts with '$' if the declaration describes an anonymous type.
	// '$s_%d' for anonymous struct types
	// '$i_%d' for anonymous interface types
	name  string
	typ   ast.Expr
	class declClass
	flags declFlags

	// functions for interface type, fields+methods for struct type
	children map[string]*decl

	// embedded types
	embedded []ast.Expr

	// if the type is unknown at AST building time, I'm using these
	value ast.Expr

	// if it's a multiassignment and the Value is a CallExpr, it is being set
	// to an index into the return value tuple, otherwise it's a -1
	valueIndex int

	// scope where this Decl was declared in (not its visibilty scope!)
	// Decl uses it for type inference
	scope *scope
}

func astDeclType(d ast.Decl) ast.Expr {
	switch t := d.(type) {
	case *ast.GenDecl:
		switch t.Tok {
		case token.CONST, token.VAR:
			c := t.Specs[0].(*ast.ValueSpec)
			return c.Type
		case token.TYPE:
			t := t.Specs[0].(*ast.TypeSpec)
			return t.Type
		}
	case *ast.FuncDecl:
		return t.Type
	}
	panic("unreachable")
	return nil
}

func astDeclClass(d ast.Decl) declClass {
	switch t := d.(type) {
	case *ast.GenDecl:
		switch t.Tok {
		case token.VAR:
			return declVar
		case token.CONST:
			return declConst
		case token.TYPE:
			return declType
		}
	case *ast.FuncDecl:
		return declFunc
	}
	panic("unreachable")
}

func astDeclConvertable(d ast.Decl) bool {
	switch t := d.(type) {
	case *ast.GenDecl:
		switch t.Tok {
		case token.VAR, token.CONST, token.TYPE:
			return true
		}
	case *ast.FuncDecl:
		return true
	}
	return false
}

func astFieldListToDecls(f *ast.FieldList, class declClass, flags declFlags, scope *scope, addAnonymous bool) map[string]*decl {
	count := 0
	for _, field := range f.List {
		count += len(field.Names)
	}

	decls := make(map[string]*decl, count)
	for _, field := range f.List {
		for _, name := range field.Names {
			if flags&declForeign != 0 && !ast.IsExported(name.Name) {
				continue
			}
			d := &decl{
				name:        name.Name,
				typ:         field.Type,
				class:       class,
				flags:       flags,
				scope:       scope,
				valueIndex: -1,
			}
			decls[d.name] = d
		}

		// add anonymous field as a child (type embedding)
		if class == declVar && field.Names == nil && addAnonymous {
			tp := getTypePath(field.Type)
			if flags&declForeign != 0 && !ast.IsExported(tp.name) {
				continue
			}
			d := &decl{
				name:        tp.name,
				typ:         field.Type,
				class:       class,
				flags:       flags,
				scope:       scope,
				valueIndex: -1,
			}
			decls[d.name] = d
		}
	}
	return decls
}

func astFieldListToEmbedded(f *ast.FieldList) []ast.Expr {
	count := 0
	for _, field := range f.List {
		if field.Names == nil || field.Names[0].Name == "?" {
			count++
		}
	}

	if count == 0 {
		return nil
	}

	embedded := make([]ast.Expr, count)
	i := 0
	for _, field := range f.List {
		if field.Names == nil || field.Names[0].Name == "?" {
			embedded[i] = field.Type
			i++
		}
	}

	return embedded
}

func astTypeToEmbedded(ty ast.Expr) []ast.Expr {
	switch t := ty.(type) {
	case *ast.StructType:
		return astFieldListToEmbedded(t.Fields)
	case *ast.InterfaceType:
		return astFieldListToEmbedded(t.Methods)
	}
	return nil
}

func astTypeToChildren(ty ast.Expr, flags declFlags, scope *scope) map[string]*decl {
	switch t := ty.(type) {
	case *ast.StructType:
		return astFieldListToDecls(t.Fields, declVar, flags, scope, true)
	case *ast.InterfaceType:
		return astFieldListToDecls(t.Methods, declFunc, flags, scope, false)
	}
	return nil
}

//-------------------------------------------------------------------------
// anonymousIdGen
//
// ID generator for anonymous types (thread-safe)
//-------------------------------------------------------------------------

type anonymousIdGen struct {
	sync.Mutex
	i int
}

func (a *anonymousIdGen) gen() (id int) {
	a.Lock()
	defer a.Unlock()
	id = a.i
	a.i++
	return
}

var gAnonGen anonymousIdGen

//-------------------------------------------------------------------------

func checkForAnonType(t ast.Expr, flags declFlags, s *scope) ast.Expr {
	if t == nil {
		return nil
	}
	var name string

	switch t.(type) {
	case *ast.StructType:
		name = fmt.Sprintf("$s_%d", gAnonGen.gen())
	case *ast.InterfaceType:
		name = fmt.Sprintf("$i_%d", gAnonGen.gen())
	}

	if name != "" {
		anonymifyAst(t, flags, s)
		d := newDeclFull(name, declType, flags, t, nil, -1, s)
		s.addNamedDecl(d)
		return ast.NewIdent(name)
	}
	return t
}

//-------------------------------------------------------------------------

func newDeclFull(name string, class declClass, flags declFlags, typ, v ast.Expr, vi int, s *scope) *decl {
	d := new(decl)
	d.name = name
	d.class = class
	d.flags = flags
	d.typ = typ
	d.value = v
	d.valueIndex = vi
	d.scope = s
	d.children = astTypeToChildren(d.typ, flags, s)
	d.embedded = astTypeToEmbedded(d.typ)
	return d
}

func newDecl(name string, class declClass, scope *scope) *decl {
	decl := new(decl)
	decl.name = name
	decl.class = class
	decl.valueIndex = -1
	decl.scope = scope
	return decl
}

func newDeclVar(name string, typ ast.Expr, value ast.Expr, vindex int, scope *scope) *decl {
	if name == "_" {
		return nil
	}
	decl := new(decl)
	decl.name = name
	decl.class = declVar
	decl.typ = typ
	decl.value = value
	decl.valueIndex = vindex
	decl.scope = scope
	return decl
}

func methodOf(d ast.Decl) string {
	if t, ok := d.(*ast.FuncDecl); ok {
		if t.Recv != nil {
			switch t := t.Recv.List[0].Type.(type) {
			case *ast.StarExpr:
				return t.X.(*ast.Ident).Name
			case *ast.Ident:
				return t.Name
			default:
				return ""
			}
		}
	}
	return ""
}

func (other *decl) deepCopy() *decl {
	d := new(decl)
	d.name = other.name
	d.class = other.class
	d.typ = other.typ
	d.value = other.value
	d.valueIndex = other.valueIndex
	d.children = make(map[string]*decl, len(other.children))
	for key, value := range other.children {
		d.children[key] = value
	}
	if other.embedded != nil {
		d.embedded = make([]ast.Expr, len(other.embedded))
		copy(d.embedded, other.embedded)
	}
	d.scope = other.scope
	return d
}

func (d *decl) clearVisited() {
	d.flags &^= declVisited
}

func (d *decl) expandOrReplace(other *decl) {
	// expand only if it's a methods stub, otherwise simply copy
	if d.class != declMethodsStub && other.class != declMethodsStub {
		d = other
		return
	}

	if d.class == declMethodsStub {
		d.typ = other.typ
		d.class = other.class
	}

	if other.children != nil {
		for _, c := range other.children {
			d.addChild(c)
		}
	}

	if other.embedded != nil {
		d.embedded = other.embedded
		d.scope = other.scope
	}
}

func (d *decl) matches() bool {
	if strings.HasPrefix(d.name, "$") || d.class == declMethodsStub {
		return false
	}
	return true
}

func (d *decl) prettyPrintType(out io.Writer) {
	switch d.class {
	case declType:
		switch d.typ.(type) {
		case *ast.StructType:
			// TODO: not used due to anonymify?
			fmt.Fprintf(out, "struct")
		case *ast.InterfaceType:
			// TODO: not used due to anonymify?
			fmt.Fprintf(out, "interface")
		default:
			if d.typ != nil {
				prettyPrintTypeExpr(out, d.typ)
			}
		}
	case declVar:
		if d.typ != nil {
			prettyPrintTypeExpr(out, d.typ)
		}
	case declFunc:
		prettyPrintTypeExpr(out, d.typ)
	}
}

func (d *decl) addChild(cd *decl) {
	if d.children == nil {
		d.children = make(map[string]*decl)
	}
	d.children[cd.name] = cd
}

func checkForBuiltinFuncs(typ *ast.Ident, c *ast.CallExpr, scope *scope) (ast.Expr, *scope) {
	if strings.HasPrefix(typ.Name, "func(") {
		if t, ok := c.Fun.(*ast.Ident); ok {
			switch t.Name {
			case "new":
				if len(c.Args) > 0 {
					e := new(ast.StarExpr)
					e.X = c.Args[0]
					return e, scope
				}
			case "make":
				if len(c.Args) > 0 {
					return c.Args[0], scope
				}
			case "append":
				if len(c.Args) > 0 {
					t, scope, _ := inferType(c.Args[0], scope, -1)
					return t, scope
				}
			case "complex":
				// TODO: fix it
				return ast.NewIdent("complex"), gUniverseScope
			case "closed":
				return ast.NewIdent("bool"), gUniverseScope
			case "cap":
				return ast.NewIdent("int"), gUniverseScope
			case "copy":
				return ast.NewIdent("int"), gUniverseScope
			case "len":
				return ast.NewIdent("int"), gUniverseScope
			}
			// TODO:
			// func recover() interface{}
			// func imag(c ComplexType) FloatType
			// func real(c ComplexType) FloatType
		}
	}
	return nil, nil
}

func funcReturnType(f *ast.FuncType, index int) ast.Expr {
	if f.Results == nil {
		return nil
	}

	if index == -1 {
		return f.Results.List[0].Type
	}

	i := 0
	var field *ast.Field
	for _, field = range f.Results.List {
		if i >= index {
			return field.Type
		}
		if field.Names != nil {
			i += len(field.Names)
		} else {
			i++
		}
	}
	if i >= index {
		return field.Type
	}
	return nil
}

type typePath struct {
	pkg  string
	name string
}

func (tp *typePath) isNil() bool {
	return tp.pkg == "" && tp.name == ""
}

// converts type expressions like:
// ast.Expr
// *ast.Expr
// $ast$go/ast.Expr
// to a path that can be used to lookup a type related Decl
func getTypePath(e ast.Expr) (r typePath) {
	if e == nil {
		return typePath{"", ""}
	}

	switch t := e.(type) {
	case *ast.Ident:
		r.name = t.Name
	case *ast.StarExpr:
		r = getTypePath(t.X)
	case *ast.SelectorExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			r.pkg = ident.Name
		}
		r.name = t.Sel.Name
	}
	return
}

func lookupPath(tp typePath, scope *scope) *decl {
	if tp.isNil() {
		return nil
	}
	var decl *decl
	if tp.pkg != "" {
		decl = scope.lookup(tp.pkg)
		// return nil early if the package wasn't found but it's part
		// of the type specification
		if decl == nil {
			return nil
		}
	}

	if decl != nil {
		if tp.name != "" {
			return decl.findChild(tp.name)
		} else {
			return decl
		}
	}

	return scope.lookup(tp.name)
}

func lookupPkg(tp typePath, scope *scope) string {
	if tp.isNil() {
		return ""
	}
	if tp.pkg == "" {
		return ""
	}
	decl := scope.lookup(tp.pkg)
	if decl == nil {
		return ""
	}
	return decl.name
}

func typeToDecl(t ast.Expr, scope *scope) *decl {
	tp := getTypePath(t)
	d := lookupPath(tp, scope)
	if d != nil && d.class == declVar {
		// weird variable declaration pointing to itself
		return nil
	}
	return d
}

func exprToDecl(e ast.Expr, scope *scope) *decl {
	t, scope, _ := inferType(e, scope, -1)
	return typeToDecl(t, scope)
}

//-------------------------------------------------------------------------
// Type inference
//-------------------------------------------------------------------------

type typePredicate func(ast.Expr) bool

func advanceToType(pred typePredicate, v ast.Expr, scope *scope) (ast.Expr, *scope) {
	if pred(v) {
		return v, scope
	}

	decl := typeToDecl(v, scope)
	if decl == nil {
		return nil, nil
	}

	if decl.flags&declVisited != 0 {
		return nil, nil
	}
	decl.flags |= declVisited
	defer decl.clearVisited()

	return advanceToType(pred, decl.typ, decl.scope)
}

func advanceToStructOrInterface(decl *decl) *decl {
	if decl.flags&declVisited != 0 {
		return nil
	}
	decl.flags |= declVisited
	defer decl.clearVisited()

	if structInterfacePredicate(decl.typ) {
		return decl
	}

	decl = typeToDecl(decl.typ, decl.scope)
	if decl == nil {
		return nil
	}
	return advanceToStructOrInterface(decl)
}

func structInterfacePredicate(v ast.Expr) bool {
	switch v.(type) {
	case *ast.StructType, *ast.InterfaceType:
		return true
	}
	return false
}

func chanPredicate(v ast.Expr) bool {
	_, ok := v.(*ast.ChanType)
	return ok
}

func indexPredicate(v ast.Expr) bool {
	switch v.(type) {
	case *ast.ArrayType, *ast.MapType, *ast.Ellipsis:
		return true
	}
	return false
}

func starPredicate(v ast.Expr) bool {
	_, ok := v.(*ast.StarExpr)
	return ok
}

func funcPredicate(v ast.Expr) bool {
	_, ok := v.(*ast.FuncType)
	return ok
}

func rangePredicate(v ast.Expr) bool {
	switch t := v.(type) {
	case *ast.Ident:
		if t.Name == "string" {
			return true
		}
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.Ellipsis:
		return true
	}
	return false
}

type anonymousTyper struct {
	flags declFlags
	scope *scope
}

func (a *anonymousTyper) Visit(node ast.Node) ast.Visitor {
	switch t := node.(type) {
	case *ast.CompositeLit:
		t.Type = checkForAnonType(t.Type, a.flags, a.scope)
	case *ast.MapType:
		t.Key = checkForAnonType(t.Key, a.flags, a.scope)
		t.Value = checkForAnonType(t.Value, a.flags, a.scope)
	case *ast.ArrayType:
		t.Elt = checkForAnonType(t.Elt, a.flags, a.scope)
	case *ast.Ellipsis:
		t.Elt = checkForAnonType(t.Elt, a.flags, a.scope)
	case *ast.ChanType:
		t.Value = checkForAnonType(t.Value, a.flags, a.scope)
	case *ast.Field:
		t.Type = checkForAnonType(t.Type, a.flags, a.scope)
	case *ast.CallExpr:
		t.Fun = checkForAnonType(t.Fun, a.flags, a.scope)
	case *ast.ParenExpr:
		t.X = checkForAnonType(t.X, a.flags, a.scope)
	case *ast.GenDecl:
		switch t.Tok {
		case token.VAR:
			for _, s := range t.Specs {
				vs := s.(*ast.ValueSpec)
				vs.Type = checkForAnonType(vs.Type, a.flags, a.scope)
			}
		}
	}
	return a
}

func anonymifyAst(node ast.Node, flags declFlags, scope *scope) {
	v := anonymousTyper{flags, scope}
	ast.Walk(&v, node)
}

// RETURNS:
// 	- type expression which represents a full name of a type
//	- bool whether a type expression is actually a type (used internally)
//	- scope in which type makes sense
func inferType(v ast.Expr, scope *scope, index int) (ast.Expr, *scope, bool) {
	switch t := v.(type) {
	case *ast.CompositeLit:
		return t.Type, scope, true
	case *ast.Ident:
		if d := scope.lookup(t.Name); d != nil {
			if d.class == declPackage {
				return ast.NewIdent(t.Name), scope, false
			}
			typ, scope := d.inferType()
			return typ, scope, d.class == declType
		}
	case *ast.UnaryExpr:
		switch t.Op {
		case token.AND:
			// &a makes sense only with values, don't even check for type
			it, s, _ := inferType(t.X, scope, -1)
			if it == nil {
				break
			}

			e := new(ast.StarExpr)
			e.X = it
			return e, s, false
		case token.ARROW:
			// <-a makes sense only with values
			it, s, _ := inferType(t.X, scope, -1)
			if it == nil {
				break
			}
			switch index {
			case -1, 0:
				it, s = advanceToType(chanPredicate, it, s)
				return it.(*ast.ChanType).Value, s, false
			case 1:
				// technically it's a value, but in case of index == 1
				// it is always the last infer operation
				return ast.NewIdent("bool"), gUniverseScope, false
			}
		case token.ADD, token.NOT, token.SUB, token.XOR:
			it, s, _ := inferType(t.X, scope, -1)
			if it == nil {
				break
			}
			return it, s, false
		}
	case *ast.BinaryExpr:
		switch t.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ,
			token.GTR, token.GEQ, token.LOR, token.LAND:
			// logic operations, the result is a bool, always
			return ast.NewIdent("bool"), gUniverseScope, false
		case token.ADD, token.SUB, token.MUL, token.QUO, token.OR,
			token.XOR, token.REM, token.AND, token.AND_NOT:
			// try X, then Y, they should be the same anyway
			it, s, _ := inferType(t.X, scope, -1)
			if it == nil {
				it, s, _ = inferType(t.Y, scope, -1)
				if it == nil {
					break
				}
			}
			return it, s, false
		case token.SHL, token.SHR:
			// try only X for shifts, Y is always uint
			it, s, _ := inferType(t.X, scope, -1)
			if it == nil {
				break
			}
			return it, s, false
		}
	case *ast.IndexExpr:
		// something[another] always returns a value and it works on a value too
		it, s, _ := inferType(t.X, scope, -1)
		if it == nil {
			break
		}
		it, s = advanceToType(indexPredicate, it, s)
		switch t := it.(type) {
		case *ast.ArrayType:
			return t.Elt, s, false
		case *ast.Ellipsis:
			return t.Elt, s, false
		case *ast.MapType:
			switch index {
			case -1, 0:
				return t.Value, s, false
			case 1:
				return ast.NewIdent("bool"), gUniverseScope, false
			}
		}
	case *ast.SliceExpr:
		// something[start : end] always returns a value
		it, s, _ := inferType(t.X, scope, -1)
		if it == nil {
			break
		}
		it, s = advanceToType(indexPredicate, it, s)
		switch t := it.(type) {
		case *ast.ArrayType:
			e := new(ast.ArrayType)
			e.Elt = t.Elt
			return e, s, false
		}
	case *ast.StarExpr:
		it, s, isType := inferType(t.X, scope, -1)
		if it == nil {
			break
		}
		if isType {
			// if it's a type, add * modifier, make it a 'pointer of' type
			e := new(ast.StarExpr)
			e.X = it
			return e, s, true
		} else {
			it, s := advanceToType(starPredicate, it, s)
			if se, ok := it.(*ast.StarExpr); ok {
				return se.X, s, false
			}
		}
	case *ast.CallExpr:
		// this is a function call or a type cast:
		// myFunc(1,2,3) or int16(myvar)
		it, s, isType := inferType(t.Fun, scope, -1)
		if it == nil {
			break
		}

		if isType {
			// a type cast
			return it, scope, false
		} else {
			// it must be a function call or a built-in function
			// first check for built-in
			if ct, ok := it.(*ast.Ident); ok {
				ty, s := checkForBuiltinFuncs(ct, t, scope)
				if ty != nil {
					return ty, s, false
				}
			}

			// then check for an ordinary function call
			it, scope = advanceToType(funcPredicate, it, s)
			if ct, ok := it.(*ast.FuncType); ok {
				return funcReturnType(ct, index), s, false
			}
		}
	case *ast.ParenExpr:
		it, s, isType := inferType(t.X, scope, -1)
		if it == nil {
			break
		}
		return it, s, isType
	case *ast.SelectorExpr:
		it, s, _ := inferType(t.X, scope, -1)
		if it == nil {
			break
		}

		if d := typeToDecl(it, s); d != nil {
			c := d.findChildAndInEmbedded(t.Sel.Name)
			if c != nil {
				if c.class == declType {
					return t, scope, true
				} else {
					typ, s := c.inferType()
					return typ, s, false
				}
			}
		}
	case *ast.FuncLit:
		// it's a value, but I think most likely we don't even care, cause we can only
		// call it, and CallExpr uses the type itself to figure out
		return t.Type, scope, false
	case *ast.TypeAssertExpr:
		if t.Type == nil {
			return inferType(t.X, scope, -1)
		}
		switch index {
		case -1, 0:
			// converting a value to a different type, but return thing is a value
			it, _, _ := inferType(t.Type, scope, -1)
			return it, scope, false
		case 1:
			return ast.NewIdent("bool"), gUniverseScope, false
		}
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.Ellipsis,
		*ast.FuncType, *ast.StructType, *ast.InterfaceType:
		return t, scope, true
	default:
		_ = reflect.TypeOf(v)
		//fmt.Println(ty)
	}
	return nil, nil, false
}

// Uses Value, ValueIndex and Scope to infer the type of this
// declaration. Returns the type itself and the scope where this type
// makes sense.
func (d *decl) inferType() (ast.Expr, *scope) {
	// special case for range vars
	if d.flags&declRangevar != 0 {
		var scope *scope
		d.typ, scope = inferRangeType(d.value, d.scope, d.valueIndex)
		return d.typ, scope
	}

	switch d.class {
	case declPackage:
		// package is handled specially in inferType
		return nil, nil
	case declType:
		return ast.NewIdent(d.name), d.scope
	}

	// shortcut
	if d.typ != nil && d.value == nil {
		return d.typ, d.scope
	}

	// prevent loops
	if d.flags&declVisited != 0 {
		return nil, nil
	}
	d.flags |= declVisited
	defer d.clearVisited()

	var scope *scope
	d.typ, scope, _ = inferType(d.value, d.scope, d.valueIndex)
	return d.typ, scope
}

func (d *decl) findChild(name string) *decl {
	if d.flags&declVisited != 0 {
		return nil
	}
	d.flags |= declVisited
	defer d.clearVisited()

	if d.children != nil {
		if c, ok := d.children[name]; ok {
			return c
		}
	}

	decl := advanceToStructOrInterface(d)
	if decl != nil && decl != d {
		return decl.findChild(name)
	}
	return nil
}

func (d *decl) findChildAndInEmbedded(name string) *decl {
	c := d.findChild(name)
	if c == nil {
		for _, e := range d.embedded {
			typedecl := typeToDecl(e, d.scope)
			c = typedecl.findChildAndInEmbedded(name)
			if c != nil {
				break
			}
		}
	}
	return c
}

// Special type inference for range statements.
// [int], [int] := range [string]
// [int], [value] := range [slice or array]
// [key], [value] := range [map]
// [value], [nil] := range [chan]
func inferRangeType(e ast.Expr, sc *scope, valueindex int) (ast.Expr, *scope) {
	t, s, _ := inferType(e, sc, -1)
	t, s = advanceToType(rangePredicate, t, s)
	if t != nil {
		var t1, t2 ast.Expr
		var s1, s2 *scope
		s1 = s
		s2 = s

		switch t := t.(type) {
		case *ast.Ident:
			// string
			if t.Name == "string" {
				t1 = ast.NewIdent("int")
				t2 = ast.NewIdent("rune")
				s1 = gUniverseScope
				s2 = gUniverseScope
			} else {
				t1, t2 = nil, nil
			}
		case *ast.ArrayType:
			t1 = ast.NewIdent("int")
			s1 = gUniverseScope
			t2 = t.Elt
		case *ast.Ellipsis:
			t1 = ast.NewIdent("int")
			s1 = gUniverseScope
			t2 = t.Elt
		case *ast.MapType:
			t1 = t.Key
			t2 = t.Value
		case *ast.ChanType:
			t1 = t.Value
			t2 = nil
		default:
			t1, t2 = nil, nil
		}

		switch valueindex {
		case 0:
			return t1, s1
		case 1:
			return t2, s2
		}
	}
	return nil, nil
}

//-------------------------------------------------------------------------
// Pretty printing
//-------------------------------------------------------------------------

func getArrayLen(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.BasicLit:
		return string(t.Value)
	case *ast.Ellipsis:
		return "..."
	}
	return ""
}

func prettyPrintTypeExpr(out io.Writer, e ast.Expr) {
	switch t := e.(type) {
	case *ast.StarExpr:
		fmt.Fprintf(out, "*")
		prettyPrintTypeExpr(out, t.X)
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
		} else if strings.HasPrefix(t.Name, "#") {
			fmt.Fprintf(out, t.Name[1:])
		} else {
			fmt.Fprintf(out, t.Name)
		}
	case *ast.ArrayType:
		al := ""
		if t.Len != nil {
			al = getArrayLen(t.Len)
		}
		if al != "" {
			fmt.Fprintf(out, "[%s]", al)
		} else {
			fmt.Fprintf(out, "[]")
		}
		prettyPrintTypeExpr(out, t.Elt)
	case *ast.SelectorExpr:
		prettyPrintTypeExpr(out, t.X)
		fmt.Fprintf(out, ".%s", t.Sel.Name)
	case *ast.FuncType:
		fmt.Fprintf(out, "func(")
		prettyPrintFuncFieldList(out, t.Params)
		fmt.Fprintf(out, ")")

		buf := bytes.NewBuffer(make([]byte, 0, 256))
		nresults := prettyPrintFuncFieldList(buf, t.Results)
		if nresults > 0 {
			results := buf.String()
			if strings.IndexAny(results, ", ") != -1 {
				results = "(" + results + ")"
			}
			fmt.Fprintf(out, " %s", results)
		}
	case *ast.MapType:
		fmt.Fprintf(out, "map[")
		prettyPrintTypeExpr(out, t.Key)
		fmt.Fprintf(out, "]")
		prettyPrintTypeExpr(out, t.Value)
	case *ast.InterfaceType:
		fmt.Fprintf(out, "interface{}")
	case *ast.Ellipsis:
		fmt.Fprintf(out, "...")
		prettyPrintTypeExpr(out, t.Elt)
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
		prettyPrintTypeExpr(out, t.Value)
	case *ast.ParenExpr:
		fmt.Fprintf(out, "(")
		prettyPrintTypeExpr(out, t.X)
		fmt.Fprintf(out, ")")
	case *ast.BadExpr:
		// TODO: probably I should check that in a separate function
		// and simply discard declarations with BadExpr as a part of their
		// type
	default:
		// the element has some weird type, just ignore it
	}
}

func prettyPrintFuncFieldList(out io.Writer, f *ast.FieldList) int {
	count := 0
	if f == nil {
		return count
	}
	for i, field := range f.List {
		// names
		if field.Names != nil {
			hasNonblank := false
			for j, name := range field.Names {
				if name.Name != "?" {
					hasNonblank = true
					fmt.Fprintf(out, "%s", name.Name)
					if j != len(field.Names)-1 {
						fmt.Fprintf(out, ", ")
					}
				}
				count++
			}
			if hasNonblank {
				fmt.Fprintf(out, " ")
			}
		} else {
			count++
		}

		// type
		prettyPrintTypeExpr(out, field.Type)

		// ,
		if i != len(f.List)-1 {
			fmt.Fprintf(out, ", ")
		}
	}
	return count
}

func astDeclNames(d ast.Decl) []*ast.Ident {
	var names []*ast.Ident

	switch t := d.(type) {
	case *ast.GenDecl:
		switch t.Tok {
		case token.CONST:
			c := t.Specs[0].(*ast.ValueSpec)
			names = make([]*ast.Ident, len(c.Names))
			for i, name := range c.Names {
				names[i] = name
			}
		case token.TYPE:
			t := t.Specs[0].(*ast.TypeSpec)
			names = make([]*ast.Ident, 1)
			names[0] = t.Name
		case token.VAR:
			v := t.Specs[0].(*ast.ValueSpec)
			names = make([]*ast.Ident, len(v.Names))
			for i, name := range v.Names {
				names[i] = name
			}
		}
	case *ast.FuncDecl:
		names = make([]*ast.Ident, 1)
		names[0] = t.Name
	}

	return names
}

func astDeclValues(d ast.Decl) []ast.Expr {
	// TODO: CONST values here too
	switch t := d.(type) {
	case *ast.GenDecl:
		switch t.Tok {
		case token.VAR:
			v := t.Specs[0].(*ast.ValueSpec)
			if v.Values != nil {
				return v.Values
			}
		}
	}
	return nil
}

func astDeclSplit(d ast.Decl) []ast.Decl {
	var decls []ast.Decl
	if t, ok := d.(*ast.GenDecl); ok {
		decls = make([]ast.Decl, len(t.Specs))
		for i, s := range t.Specs {
			decl := new(ast.GenDecl)
			*decl = *t
			decl.Specs = make([]ast.Spec, 1)
			decl.Specs[0] = s
			decls[i] = decl
		}
	} else {
		decls = make([]ast.Decl, 1)
		decls[0] = d
	}
	return decls
}

//-------------------------------------------------------------------------
// declPack
//-------------------------------------------------------------------------

type declPack struct {
	names  []*ast.Ident
	typ    ast.Expr
	values []ast.Expr
}

type foreachDeclStruct struct {
	declPack
	decl ast.Decl
}

func (f *declPack) value(i int) ast.Expr {
	if f.values == nil {
		return nil
	}
	if len(f.values) > 1 {
		return f.values[i]
	}
	return f.values[0]
}

func (f *declPack) valueIndex(i int) (v ast.Expr, vi int) {
	// default: nil value
	v = nil
	vi = -1

	if f.values != nil {
		// A = B, if there is only one name, the value is solo too
		if len(f.names) == 1 {
			return f.values[0], -1
		}

		if len(f.values) > 1 {
			// in case if there are multiple values, it's a usual
			// multiassignment
			v = f.values[i]
		} else {
			// in case if there is one value, but many names, it's
			// a tuple unpack.. use index here
			v = f.values[0]
			vi = i
		}
	}
	return
}

func (f *declPack) typeValueIndex(i int) (ast.Expr, ast.Expr, int) {
	if f.typ != nil {
		// If there is a type, we don't care about value, just return the type
		// and zero value.
		return f.typ, nil, -1
	}

	// And otherwise we simply return nil type and a valid value for later inferring.
	v, vi := f.valueIndex(i)
	return nil, v, vi
}

type foreachDeclFunc func(data *foreachDeclStruct)

func foreachDecl(decl ast.Decl, do foreachDeclFunc) {
	decls := astDeclSplit(decl)
	var data foreachDeclStruct
	for _, decl := range decls {
		if !astDeclConvertable(decl) {
			continue
		}
		data.names = astDeclNames(decl)
		data.typ = astDeclType(decl)
		data.values = astDeclValues(decl)
		data.decl = decl

		do(&data)
	}
}

//-------------------------------------------------------------------------
// Built-in declarations
//-------------------------------------------------------------------------

var gUniverseScope = newScope(nil)

func init() {
	builtin := ast.NewIdent("built-in")

	addType := func(name string) {
		d := newDecl(name, declType, gUniverseScope)
		d.typ = builtin
		gUniverseScope.addNamedDecl(d)
	}
	addType("bool")
	addType("byte")
	addType("complex64")
	addType("complex128")
	addType("float32")
	addType("float64")
	addType("int8")
	addType("int16")
	addType("int32")
	addType("int64")
	addType("string")
	addType("uint8")
	addType("uint16")
	addType("uint32")
	addType("uint64")
	addType("int")
	addType("uint")
	addType("uintptr")
	addType("rune")

	addConst := func(name string) {
		d := newDecl(name, declConst, gUniverseScope)
		d.typ = builtin
		gUniverseScope.addNamedDecl(d)
	}
	addConst("true")
	addConst("false")
	addConst("iota")
	addConst("nil")

	addFunc := func(name, typ string) {
		d := newDecl(name, declFunc, gUniverseScope)
		d.typ = ast.NewIdent(typ)
		gUniverseScope.addNamedDecl(d)
	}
	addFunc("append", "func([]type, ...type) []type")
	addFunc("cap", "func(container) int")
	addFunc("close", "func(channel)")
	addFunc("complex", "func(real, imag) complex")
	addFunc("copy", "func(dst, src)")
	addFunc("delete", "func(map[typeA]typeB, typeA)")
	addFunc("imag", "func(complex)")
	addFunc("len", "func(container) int")
	addFunc("make", "func(type, len[, cap]) type")
	addFunc("new", "func(type) *type")
	addFunc("panic", "func(interface{})")
	addFunc("print", "func(...interface{})")
	addFunc("println", "func(...interface{})")
	addFunc("real", "func(complex)")
	addFunc("recover", "func() interface{}")

	// built-in error interface
	d := newDecl("error", declType, gUniverseScope)
	d.typ = &ast.InterfaceType{}
	d.children = make(map[string]*decl)
	d.children["Error"] = newDecl("Error", declFunc, gUniverseScope)
	d.children["Error"].typ = &ast.FuncType{
		Results: &ast.FieldList{
			List: []*ast.Field{
				{
					Type: ast.NewIdent("string"),
				},
			},
		},
	}
	gUniverseScope.addNamedDecl(d)
}
