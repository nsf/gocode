package main

import (
	"go/ast"
	"go/token"
	"fmt"
	"reflect"
	"io"
	"bytes"
	"strings"
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
		for _, name := range field.Names {
			if flags&DECL_FOREIGN != 0 && !ast.IsExported(name.Name) {
				continue
			}
			d := new(Decl)
			d.Name = name.Name
			d.Type = field.Type
			d.Class = int16(class)
			d.Flags = int16(flags)
			d.Children = astTypeToChildren(field.Type, flags, scope)
			d.Embedded = astTypeToEmbedded(field.Type)
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
			d.Type = field.Type
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

func NewDeclFromAstDecl(name string, flags int, d ast.Decl, value ast.Expr, vindex int, scope *Scope) *Decl {
	if !astDeclConvertable(d) || name == "_" {
		return nil
	}
	decl := new(Decl)
	decl.Name = name
	decl.Type = astDeclType(d)
	decl.Class = int16(astDeclClass(d))
	decl.Flags = int16(flags)
	decl.Children = astTypeToChildren(decl.Type, flags, scope)
	decl.Embedded = astTypeToEmbedded(decl.Type)
	decl.Value = value
	decl.ValueIndex = vindex
	decl.Scope = scope
	return decl
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
	decl.Children = astTypeToChildren(decl.Type, 0, scope)
	decl.Embedded = astTypeToEmbedded(decl.Type)
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

func (d *Decl) MoveToScope(scope *Scope) {
	d.Scope = scope
	for _, c := range d.Children {
		c.MoveToScope(scope)
	}
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

func (d *Decl) Matches(p string) bool {
	if p != "" && !strings.HasPrefix(d.Name, p) {
		return false
	}
	if d.Class == DECL_METHODS_STUB {
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

func checkForBuiltinFuncs(typ *ast.Ident, c *ast.CallExpr) ast.Expr {
	if strings.HasPrefix(typ.Name, "func(") {
		if t, ok := c.Fun.(*ast.Ident); ok {
			switch t.Name {
			case "new":
				e := new(ast.StarExpr)
				e.X = c.Args[0]
				return e
			case "make":
				return c.Args[0]
			case "cmplx":
				return ast.NewIdent("complex")
			case "closed":
				return ast.NewIdent("bool")
			}
		}
	}
	return nil
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

	var typedecl *Decl
	switch t.(type) {
	case *ast.StructType, *ast.InterfaceType:
		typedecl = NewDeclVar("tmp", t, nil, -1, scope)
	default:
		typedecl = typeToDecl(t, scope)
	}
	return typedecl
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
				return ast.NewIdent("bool"), s, false // TODO: return real built-in bool here
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
				return ast.NewIdent("bool"), s, false // TODO: return real built-in bool here
			}
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
				ty := checkForBuiltinFuncs(ct, t)
				if ty != nil {
					return ty, scope, false
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

		var d *Decl
		switch it.(type) {
		case *ast.StructType, *ast.InterfaceType:
			d = NewDeclVar("tmp", it, nil, -1, s)
		default:
			d = typeToDecl(it, s)
		}

		if d != nil {
			c := d.FindChildAndInEmbedded(t.Sel.Name)
			if c != nil {
				if c.Class == DECL_TYPE {
					// use foregnified module name
					//t.X = ast.NewIdent(d.Name)
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
			return ast.NewIdent("bool"), scope, false // TODO: return real built-in bool here
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
	t, s = advanceToType(rangePredicate, e, scope)
	if t != nil {
		var t1, t2 ast.Expr
		switch t := t.(type) {
		case *ast.Ident:
			// string
			if t.Name == "string" {
				t1 = ast.NewIdent("int")
				t2 = ast.NewIdent("int")
			} else {
				t1, t2 = nil, nil
			}
		case *ast.ArrayType:
			t1 = ast.NewIdent("int")
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
			return t1, s
		case 1:
			return t2, s
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
		fmt.Fprintf(out, t.Name)
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

func declNames(d ast.Decl) []string {
	var names []string

	switch t := d.(type) {
	case *ast.GenDecl:
		switch t.Tok {
		case token.CONST:
			c := t.Specs[0].(*ast.ValueSpec)
			names = make([]string, len(c.Names))
			for i, name := range c.Names {
				names[i] = name.Name
			}
		case token.TYPE:
			t := t.Specs[0].(*ast.TypeSpec)
			names = make([]string, 1)
			names[0] = t.Name.Name
		case token.VAR:
			v := t.Specs[0].(*ast.ValueSpec)
			names = make([]string, len(v.Names))
			for i, name := range v.Names {
				names[i] = name.Name
			}
		}
	case *ast.FuncDecl:
		names = make([]string, 1)
		names[0] = t.Name.Name
	}

	return names
}

func declValues(d ast.Decl) []ast.Expr {
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

func splitDecls(d ast.Decl) []ast.Decl {
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

type foreachDeclFunc func(ast.Decl, string, ast.Expr, int)

func foreachDecl(decl ast.Decl, do foreachDeclFunc) {
	decls := splitDecls(decl)
	for _, decl := range decls {
		names := declNames(decl)
		values := declValues(decl)

		for i, name := range names {
			var value ast.Expr
			valueindex := -1
			if values != nil {
				if len(values) > 1 {
					value = values[i]
				} else {
					value = values[0]
					valueindex = i
				}
			}

			do(decl, name, value, valueindex)
		}
	}
}
