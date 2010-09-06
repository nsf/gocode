package main

import (
	"go/ast"
	"go/token"
	"fmt"
	"reflect"
	"io"
	"bytes"
	"strings"
	"sync"
)

// Decl.Class
const (
	DECL_CONST = iota
	DECL_VAR
	DECL_TYPE
	DECL_FUNC
	DECL_MODULE

	// this one serves as a temporary type for those methods that were
	// declared before their actual owner
	DECL_METHODS_STUB
)

// Decl.Flags
const (
	DECL_FOREIGN = 1 << iota // imported from another module

	// means that the decl is a part of the range statement
	// its type is inferred in a special way
	DECL_RANGEVAR
)

var declClassToString = [...]string{
	DECL_CONST:        "const",
	DECL_VAR:          "var",
	DECL_TYPE:         "type",
	DECL_FUNC:         "func",
	DECL_MODULE:       "module",
	DECL_METHODS_STUB: "IF YOU SEE THIS, REPORT A BUG", // :D
}

type Decl struct {
	Name  string
	Type  ast.Expr
	Class int16
	Flags int16

	// functions for interface type, fields+methods for struct type
	// variables with anonymous struct/interface type may have children too
	Children map[string]*Decl

	// embedded types
	Embedded []ast.Expr

	// if the type is unknown at AST building time, I'm using these
	Value ast.Expr

	// if it's a multiassignment and the Value is a CallExpr, it is being set
	// to an index into the return value tuple, otherwise it's a -1
	ValueIndex int

	// scope where this Decl was declared in (not its visibilty scope!)
	// Decl uses it for type inference
	Scope *Scope
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

func astDeclClass(d ast.Decl) int {
	switch t := d.(type) {
	case *ast.GenDecl:
		switch t.Tok {
		case token.VAR:
			return DECL_VAR
		case token.CONST:
			return DECL_CONST
		case token.TYPE:
			return DECL_TYPE
		}
	case *ast.FuncDecl:
		return DECL_FUNC
	}
	panic("unreachable")
	return 0
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

func astFieldListToDecls(f *ast.FieldList, class int, flags int, scope *Scope) map[string]*Decl {
	count := 0
	for _, field := range f.List {
		count += len(field.Names)
	}

	if count == 0 {
		return nil
	}

	decls := make(map[string]*Decl, count)
	for _, field := range f.List {
		typ := checkForAnonType(field.Type, flags, scope)
		for _, name := range field.Names {
			if flags&DECL_FOREIGN != 0 && !ast.IsExported(name.Name) {
				continue
			}
			d := new(Decl)
			d.Name = name.Name
			d.Type = typ
			d.Class = int16(class)
			d.Flags = int16(flags)
			d.Scope = scope
			d.ValueIndex = -1
			decls[d.Name] = d
		}

		// add anonymous field as a child (type embedding)
		if class == DECL_VAR && field.Names == nil {
			tp := typePath(field.Type)
			if flags&DECL_FOREIGN != 0 && !ast.IsExported(tp.name) {
				continue
			}
			d := new(Decl)
			d.Name = tp.name
			d.Type = typ
			d.Class = int16(class)
			d.Flags = int16(flags)
			d.Scope = scope
			d.ValueIndex = -1
			decls[d.Name] = d
		}
	}
	return decls
}

func astFieldListToEmbedded(f *ast.FieldList) []ast.Expr {
	count := 0
	for _, field := range f.List {
		if field.Names == nil {
			count++
		}
	}

	if count == 0 {
		return nil
	}

	embedded := make([]ast.Expr, count)
	i := 0
	for _, field := range f.List {
		if field.Names == nil {
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

func astTypeToChildren(ty ast.Expr, flags int, scope *Scope) map[string]*Decl {
	switch t := ty.(type) {
	case *ast.StructType:
		return astFieldListToDecls(t.Fields, DECL_VAR, flags, scope)
	case *ast.InterfaceType:
		return astFieldListToDecls(t.Methods, DECL_FUNC, flags, scope)
	}
	return nil
}

//-------------------------------------------------------------------------
// AnonymousIDGen
// ID generator for anonymous types (thread-safe)
//-------------------------------------------------------------------------

type AnonymousIDGen struct {
	sync.Mutex
	i int
}

func (a *AnonymousIDGen) Gen() (id int) {
	a.Lock()
	id = a.i
	a.i++
	a.Unlock()
	return
}

var anonGen AnonymousIDGen

//-------------------------------------------------------------------------

func checkForAnonType(t ast.Expr, flags int, s *Scope) ast.Expr {
	var name string

	switch t.(type) {
	case *ast.StructType:
		name = fmt.Sprintf("$s_%d", anonGen.Gen())
	case *ast.InterfaceType:
		name = fmt.Sprintf("$i_%d", anonGen.Gen())
	}

	if name != "" {
		d := NewDeclAnonType(name, flags, t, s)
		s.addNamedDecl(d)
		return ast.NewIdent(name)
	}
	return t
}

func checkValueForAnonType(v ast.Expr, flags int, s *Scope) ast.Expr {
	switch t := v.(type) {
	case *ast.CompositeLit:
		return checkForAnonType(t.Type, flags, s)
	}
	return nil
}

//-------------------------------------------------------------------------

func NewDecl2(name string, class, flags int, typ, v ast.Expr, vi int, s *Scope) *Decl {
	d := new(Decl)
	d.Name = name
	d.Class = int16(class)
	d.Flags = int16(flags)
	d.Type = typ
	d.Value = v
	d.ValueIndex = vi
	d.Scope = s
	d.Children = astTypeToChildren(d.Type, flags, s)
	d.Embedded = astTypeToEmbedded(d.Type)
	return d
}

func NewDeclAnonType(name string, flags int, typ ast.Expr, s *Scope) *Decl {
	d := NewDecl(name, DECL_TYPE, s)
	d.Type = typ
	d.Flags = int16(flags)
	d.Children = astTypeToChildren(d.Type, flags, s)
	d.Embedded = astTypeToEmbedded(d.Type)
	return d
}

func NewDeclTyped(name string, class int, typ ast.Expr, scope *Scope) *Decl {
	d := NewDecl(name, class, scope)
	d.Type = typ
	return d
}

func NewDeclTypedNamed(name string, class int, typ string, scope *Scope) *Decl {
	d := NewDecl(name, class, scope)
	d.Type = ast.NewIdent(typ)
	return d
}

func NewDecl(name string, class int, scope *Scope) *Decl {
	decl := new(Decl)
	decl.Name = name
	decl.Class = int16(class)
	decl.ValueIndex = -1
	decl.Scope = scope
	return decl
}

func NewDeclVar(name string, typ ast.Expr, value ast.Expr, vindex int, scope *Scope) *Decl {
	if name == "_" {
		return nil
	}
	decl := new(Decl)
	decl.Name = name
	decl.Class = DECL_VAR
	decl.Type = typ
	decl.Value = value
	decl.ValueIndex = vindex
	decl.Scope = scope
	return decl
}

func MethodOf(d ast.Decl) string {
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

// complete copy
func (d *Decl) Copy(other *Decl) {
	d.Name = other.Name
	d.Class = other.Class
	d.Type = other.Type
	d.Value = other.Value
	d.ValueIndex = other.ValueIndex
	d.Children = other.Children
	d.Embedded = other.Embedded
	d.Scope = other.Scope
}

func (other *Decl) DeepCopy() *Decl {
	d := new(Decl)
	d.Name = other.Name
	d.Class = other.Class
	d.Type = other.Type
	d.Value = other.Value
	d.ValueIndex = other.ValueIndex
	d.Children = make(map[string]*Decl, len(other.Children))
	for key, value := range other.Children {
		d.Children[key] = value
	}
	if other.Embedded != nil {
		d.Embedded = make([]ast.Expr, len(other.Embedded))
		copy(d.Embedded, other.Embedded)
	}
	d.Scope = other.Scope
	return d
}

func (d *Decl) ClassName() string {
	return declClassToString[d.Class]
}

func (d *Decl) ExpandOrReplace(other *Decl) {
	// expand only if it's a methods stub, otherwise simply copy
	if d.Class != DECL_METHODS_STUB && other.Class != DECL_METHODS_STUB {
		d.Copy(other)
		return
	}

	if d.Class == DECL_METHODS_STUB {
		d.Type = other.Type
		d.Class = other.Class
	}

	if other.Children != nil {
		for _, c := range other.Children {
			d.AddChild(c)
		}
	}

	if other.Embedded != nil {
		d.Embedded = other.Embedded
		d.Scope = other.Scope
	}
}

func (d *Decl) Matches() bool {
	if strings.HasPrefix(d.Name, "$") || d.Class == DECL_METHODS_STUB {
		return false
	}
	return true
}

func (d *Decl) PrettyPrintType(out io.Writer) {
	switch d.Class {
	case DECL_TYPE:
		switch t := d.Type.(type) {
		case *ast.StructType:
			fmt.Fprintf(out, "struct")
		case *ast.InterfaceType:
			fmt.Fprintf(out, "interface")
		default:
			if d.Type != nil {
				prettyPrintTypeExpr(out, d.Type)
			}
		}
	case DECL_VAR:
		if d.Type != nil {
			prettyPrintTypeExpr(out, d.Type)
		}
	case DECL_FUNC:
		prettyPrintTypeExpr(out, d.Type)
	}
}

func (d *Decl) AddChild(cd *Decl) {
	if d.Children == nil {
		d.Children = make(map[string]*Decl)
	}
	d.Children[cd.Name] = cd
}

func checkForBuiltinFuncs(typ *ast.Ident, c *ast.CallExpr, scope *Scope) (ast.Expr, *Scope) {
	if strings.HasPrefix(typ.Name, "func(") {
		if t, ok := c.Fun.(*ast.Ident); ok {
			switch t.Name {
			case "new":
				e := new(ast.StarExpr)
				e.X = c.Args[0]
				return e, scope
			case "make":
				return c.Args[0], scope
			case "cmplx":
				return ast.NewIdent("complex"), universeScope
			case "closed":
				return ast.NewIdent("bool"), universeScope
			}
		}
	}
	return nil, nil
}

func funcReturnType(f *ast.FuncType, index int) ast.Expr {
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

type TypePath struct {
	module string
	name   string
}

func (tp *TypePath) IsNil() bool {
	return tp.module == "" && tp.name == ""
}

// converts type expressions like:
// ast.Expr
// *ast.Expr
// $ast$go/ast.Expr
// to a path that can be used to lookup a type related Decl
func typePath(e ast.Expr) (r TypePath) {
	if e == nil {
		return TypePath{"", ""}
	}

	switch t := e.(type) {
	case *ast.Ident:
		r.name = t.Name
	case *ast.StarExpr:
		r = typePath(t.X)
	case *ast.SelectorExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			r.module = ident.Name
		}
		r.name = t.Sel.Name
	}
	return
}

func lookupPath(tp TypePath, scope *Scope) *Decl {
	if tp.IsNil() {
		return nil
	}
	var decl *Decl
	if tp.module != "" {
		decl = scope.lookup(tp.module)
	}

	if decl != nil {
		if tp.name != "" {
			return decl.FindChild(tp.name)
		} else {
			return decl
		}
	}

	return scope.lookup(tp.name)
}

func typeToDecl(t ast.Expr, scope *Scope) *Decl {
	tp := typePath(t)
	return lookupPath(tp, scope)
}

func exprToDecl(e ast.Expr, scope *Scope) *Decl {
	t, scope, _ := inferType(e, scope, -1)
	return typeToDecl(t, scope)
}

//-------------------------------------------------------------------------
// Type inference
//-------------------------------------------------------------------------

type TypePredicate func(ast.Expr) bool

func advanceToType(pred TypePredicate, v ast.Expr, scope *Scope) (ast.Expr, *Scope) {
	if pred(v) {
		return v, scope
	}

	for {
		tp := typePath(v)
		if tp.IsNil() {
			return nil, nil
		}
		decl := lookupPath(tp, scope)
		if decl == nil {
			return nil, nil
		}

		v = decl.Type
		scope = decl.Scope
		if pred(v) {
			break
		}
	}
	return v, scope
}

func chanPredicate(v ast.Expr) bool {
	_, ok := v.(*ast.ChanType)
	return ok
}

func indexPredicate(v ast.Expr) bool {
	switch v.(type) {
	case *ast.ArrayType, *ast.MapType:
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
	case *ast.ArrayType, *ast.MapType, *ast.ChanType:
		return true
	}
	return false
}

// RETURNS:
// 	- type expression which represents a full name of a type
//	- bool whether a type expression is actually a type (used internally)
//	- scope in which type makes sense
func inferType(v ast.Expr, scope *Scope, index int) (ast.Expr, *Scope, bool) {
	switch t := v.(type) {
	case *ast.CompositeLit:
		return t.Type, scope, true
	case *ast.Ident:
		if d := scope.lookup(t.Name); d != nil {
			if d.Class == DECL_MODULE {
				return ast.NewIdent(t.Name), scope, false
			}
			typ, scope := d.InferType()
			return typ, scope, d.Class == DECL_TYPE
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
				return ast.NewIdent("bool"), universeScope, false
			}
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
		case *ast.MapType:
			switch index {
			case -1, 0:
				return t.Value, s, false
			case 1:
				return ast.NewIdent("bool"), universeScope, false
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
			c := d.FindChildAndInEmbedded(t.Sel.Name)
			if c != nil {
				if c.Class == DECL_TYPE {
					return t, scope, true
				} else {
					typ, s := c.InferType()
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
			return ast.NewIdent("bool"), universeScope, false
		}
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType:
		return t, scope, true
	default:
		_ = reflect.Typeof(v)
		//fmt.Println(ty)
	}
	return nil, nil, false
}

func (d *Decl) InferType() (ast.Expr, *Scope) {
	// special case for range vars
	if d.Flags&DECL_RANGEVAR != 0 {
		var scope *Scope
		d.Type, scope = inferRangeType(d.Value, d.Scope, d.ValueIndex)
		return d.Type, scope
	}

	switch d.Class {
	case DECL_MODULE:
		// module is handled specially in inferType
		return nil, nil
	case DECL_TYPE:
		return ast.NewIdent(d.Name), d.Scope
	}

	// shortcut
	if d.Type != nil && d.Value == nil {
		return d.Type, d.Scope
	}

	var scope *Scope
	d.Type, scope, _ = inferType(d.Value, d.Scope, d.ValueIndex)
	return d.Type, scope
}

func (d *Decl) FindChild(name string) *Decl {
	if d.Children == nil {
		return nil
	}
	if c, ok := d.Children[name]; ok {
		return c
	}
	return nil
}

func (d *Decl) FindChildAndInEmbedded(name string) *Decl {
	c := d.FindChild(name)
	if c == nil {
		for _, e := range d.Embedded {
			typedecl := typeToDecl(e, d.Scope)
			c = typedecl.FindChildAndInEmbedded(name)
			if c != nil {
				break
			}
		}
	}
	return c
}

func inferRangeType(e ast.Expr, scope *Scope, valueindex int) (ast.Expr, *Scope) {
	t, s, _ := inferType(e, scope, -1)
	t, s = advanceToType(rangePredicate, t, s)
	if t != nil {
		var t1, t2 ast.Expr
		var s1, s2 *Scope
		s1 = s
		s2 = s

		switch t := t.(type) {
		case *ast.Ident:
			// string
			if t.Name == "string" {
				t1 = ast.NewIdent("int")
				t2 = ast.NewIdent("int")
				s1 = universeScope
				s2 = universeScope
			} else {
				t1, t2 = nil, nil
			}
		case *ast.ArrayType:
			t1 = ast.NewIdent("int")
			s1 = universeScope
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
				fmt.Fprintf(out, "interface")
			}
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
			if strings.Index(results, ",") != -1 {
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
	case *ast.BadExpr:
		// TODO: probably I should check that in a separate function
		// and simply discard declarations with BadExpr as a part of their
		// type
	default:
		// should never happen
		ty := reflect.Typeof(t)
		s := fmt.Sprintf("unknown type: %s\n", ty.String())
		panic(s)
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
			for j, name := range field.Names {
				fmt.Fprintf(out, "%s", name.Name)
				if j != len(field.Names)-1 {
					fmt.Fprintf(out, ", ")
				}
				count++
			}
			fmt.Fprintf(out, " ")
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

type declPack struct {
	names []*ast.Ident
	typ ast.Expr
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

func (f *declPack) tryMakeAnonType(class, flags int, scope *Scope) {
	if class == DECL_VAR && f.typ != nil {
		f.typ = checkForAnonType(f.typ, flags, scope)
	}
}

func (f *declPack) tryMakeAnonTypeFromValue(i, flags int, scope *Scope) ast.Expr {
	v, vi := f.valueIndex(i)
	if v != nil && vi == -1 {
		return checkValueForAnonType(v, flags, scope)
	}
	return nil
}

func (f *declPack) typeValueIndex(i, flags int, scope *Scope) (ast.Expr, ast.Expr, int) {
	if f.typ != nil {
		// If there is a type, we don't care about value, just return the type
		// and zero value.
		return f.typ, nil, -1
	}
	// Otherwise the type is being introduced by the value and we need to check
	// for anonymous type here. If value introduces anonymous type we use it.
	typ := f.tryMakeAnonTypeFromValue(i, flags, scope)
	if typ != nil {
		return typ, nil, -1
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

var universeScope = NewScope(nil)

func init() {
	t := ast.NewIdent("built-in")
	u := universeScope
	u.addNamedDecl(NewDeclTyped("bool", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("byte", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("complex64", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("complex128", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("float32", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("float64", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("int8", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("int16", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("int32", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("int64", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("string", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("uint8", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("uint16", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("uint32", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("uint64", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("complex", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("float", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("int", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("uint", DECL_TYPE, t, u))
	u.addNamedDecl(NewDeclTyped("uintptr", DECL_TYPE, t, u))

	u.addNamedDecl(NewDeclTyped("true", DECL_CONST, t, u))
	u.addNamedDecl(NewDeclTyped("false", DECL_CONST, t, u))
	u.addNamedDecl(NewDeclTyped("iota", DECL_CONST, t, u))
	u.addNamedDecl(NewDeclTyped("nil", DECL_CONST, t, u))

	u.addNamedDecl(NewDeclTypedNamed("cap", DECL_FUNC, "func(container) int", u))
	u.addNamedDecl(NewDeclTypedNamed("close", DECL_FUNC, "func(channel)", u))
	u.addNamedDecl(NewDeclTypedNamed("closed", DECL_FUNC, "func(channel) bool", u))
	u.addNamedDecl(NewDeclTypedNamed("cmplx", DECL_FUNC, "func(real, imag)", u))
	u.addNamedDecl(NewDeclTypedNamed("copy", DECL_FUNC, "func(dst, src)", u))
	u.addNamedDecl(NewDeclTypedNamed("imag", DECL_FUNC, "func(complex)", u))
	u.addNamedDecl(NewDeclTypedNamed("len", DECL_FUNC, "func(container) int", u))
	u.addNamedDecl(NewDeclTypedNamed("make", DECL_FUNC, "func(type, len[, cap]) type", u))
	u.addNamedDecl(NewDeclTypedNamed("new", DECL_FUNC, "func(type) *type", u))
	u.addNamedDecl(NewDeclTypedNamed("panic", DECL_FUNC, "func(interface{})", u))
	u.addNamedDecl(NewDeclTypedNamed("print", DECL_FUNC, "func(...interface{})", u))
	u.addNamedDecl(NewDeclTypedNamed("println", DECL_FUNC, "func(...interface{})", u))
	u.addNamedDecl(NewDeclTypedNamed("real", DECL_FUNC, "func(complex)", u))
	u.addNamedDecl(NewDeclTypedNamed("recover", DECL_FUNC, "func() interface{}", u))
}
