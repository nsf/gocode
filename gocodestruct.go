package main

import (
	"go/ast"
	"go/token"
	"fmt"
	"reflect"
	"io"
)

const (
	DECL_CONST = iota
	DECL_VAR
	DECL_TYPE
	DECL_FUNC
	DECL_MODULE
)

var declClassToString = map[int]string{
	0: "const",
	1: "var",
	2: "type",
	3: "func",
	4: "module",
}

type Decl struct {
	Name string
	Type ast.Expr
	Class int

	// functions for interface type, fields+methods for struct type
	// variables with anonymous struct/interface type may have children too
	Children []*Decl

	// embedded types
	Embedded []ast.Expr

	// if the type is unknown at AST building time, I'm using these
	Value ast.Expr

	// if it's a multiassignment and the Value is a CallExpr, it is being set
	// to an index into the return value tuple, otherwise it's a -1
	ValueIndex int

	File *PackageFile
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

func astFieldListToDecls(f *ast.FieldList, class int, file *PackageFile) []*Decl {
	count := 0
	for _, field := range f.List {
		count += len(field.Names)
	}

	decls := make([]*Decl, count)
	i := 0
	for _, field := range f.List {
		for _, name := range field.Names {
			decls[i] = new(Decl)
			decls[i].Name = name.Name()
			decls[i].Type = field.Type
			decls[i].Class = class
			decls[i].Children = astTypeToChildren(field.Type, file)
			decls[i].Embedded = astTypeToEmbedded(field.Type)
			decls[i].File = file
			decls[i].ValueIndex = -1
			decls[i].init()
			i++
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

func astTypeToChildren(ty ast.Expr, file *PackageFile) []*Decl {
	switch t := ty.(type) {
	case *ast.StructType:
		return astFieldListToDecls(t.Fields, DECL_VAR, file)
	case *ast.InterfaceType:
		return astFieldListToDecls(t.Methods, DECL_FUNC, file)
	}
	return nil
}

func (self *Decl) init() {
	if self.Type != nil {
		self.File.foreignifyTypeExpr(self.Type)
	}
}

func NewDeclFromAstDecl(name string, d ast.Decl, value ast.Expr, vindex int, file *PackageFile) *Decl {
	if !astDeclConvertable(d) || name == "_" {
		return nil
	}
	decl := new(Decl)
	decl.Name = name
	decl.Type = astDeclType(d)
	decl.Class = astDeclClass(d)
	decl.Children = astTypeToChildren(decl.Type, file)
	decl.Embedded = astTypeToEmbedded(decl.Type)
	decl.Value = value
	decl.ValueIndex = vindex
	decl.File = file
	decl.init()
	return decl
}

func NewDecl(name string, class int, file *PackageFile) *Decl {
	decl := new(Decl)
	decl.Name = name
	decl.Class = class
	decl.ValueIndex = -1
	decl.File = file
	return decl
}

func NewDeclVar(name string, typ ast.Expr, value ast.Expr, vindex int, file *PackageFile) *Decl {
	if name == "_" {
		return nil
	}
	decl := new(Decl)
	decl.Name = name
	decl.Class = DECL_VAR
	decl.Type = typ
	decl.Children = astTypeToChildren(decl.Type, file)
	decl.Embedded = astTypeToEmbedded(decl.Type)
	decl.Value = value
	decl.ValueIndex = vindex
	decl.File = file
	decl.init()
	return decl
}

func MethodOf(d ast.Decl) string {
	if t, ok := d.(*ast.FuncDecl); ok {
		if t.Recv != nil {
			switch t := t.Recv.List[0].Type.(type) {
			case *ast.StarExpr:
				return t.X.(*ast.Ident).Name()
			case *ast.Ident:
				return t.Name()
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
	d.File = other.File
}

func (other *Decl) DeepCopy() *Decl {
	d := new(Decl)
	d.Name = other.Name
	d.Class = other.Class
	d.Type = other.Type
	d.Value = other.Value
	d.ValueIndex = other.ValueIndex
	d.Children = make([]*Decl, len(other.Children))
	copy(d.Children, other.Children)
	if other.Embedded != nil {
		d.Embedded = make([]ast.Expr, len(other.Embedded))
		copy(d.Embedded, other.Embedded)
	}
	d.File = other.File
	return d
}

func (d *Decl) ClassName() string {
	return declClassToString[d.Class]
}

func (d *Decl) Expand(other *Decl) {
	// in case if it's a variable, just replace an old one with a new one
	if d.Class == DECL_VAR {
		d.Copy(other)
		return
	}

	// otherwise apply Type and Class and append Children
	d.Type = other.Type
	d.Class = other.Class

	if other.Children != nil {
		for _, c := range other.Children {
			d.AddChild(c)
		}
	}

	if d.Embedded == nil && other.Embedded != nil {
		d.Embedded = other.Embedded
		d.File = other.File
	}
}

func (d *Decl) Matches(p string) bool {
	if p != "" && !startsWith(d.Name, p) {
		return false
	}
	if d.Class == DECL_TYPE && d.Type == nil {
		return false
	}
	return true
}

func (d *Decl) PrettyPrintType(out io.Writer, ac *AutoCompleteContext) {
	switch d.Class {
	case DECL_TYPE:
		switch t := d.Type.(type) {
		case *ast.StructType:
			fmt.Fprintf(out, "struct")
		case *ast.InterfaceType:
			fmt.Fprintf(out, "interface")
		default:
			if d.Type != nil {
				ac.prettyPrintTypeExpr(out, d.Type)
			}
		}
	case DECL_VAR:
		if d.Type != nil {
			ac.prettyPrintTypeExpr(out, d.Type)
		}
	case DECL_FUNC:
		ac.prettyPrintTypeExpr(out, d.Type)
	}
}

func (d *Decl) AddChild(cd *Decl) {
	if i := d.FindChildNum(cd.Name); i != -1 {
		d.Children[i] = cd
		return
	}

	if d.Children == nil {
		d.Children = make([]*Decl, 0, 4)
	}

	if cap(d.Children) < len(d.Children)+1 {
		newcap := cap(d.Children) * 2
		if newcap == 0 {
			newcap = 4
		}

		s := make([]*Decl, len(d.Children), newcap)
		copy(s, d.Children)
		d.Children = s
	}

	i := len(d.Children)
	d.Children = d.Children[0:i+1]
	d.Children[i] = cd
}

func checkForBuiltinFuncs(c *ast.CallExpr) ast.Expr {
	if t, ok := c.Fun.(*ast.Ident); ok {
		switch t.Name() {
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

func typePath(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name()
	case *ast.StarExpr:
		return typePath(t.X)
	case *ast.SelectorExpr:
		path := ""
		if ident, ok := t.X.(*ast.Ident); ok {
			path += ident.Name()
		}
		return path + "." + t.Sel.Name()
	case *ast.StructType, *ast.InterfaceType:
		// this is an invalid identifier, means use the declaration itself
		return "0"
	}
	return ""
}

func typeToDecl(t ast.Expr, file *PackageFile) *Decl {
	name := typePath(t)
	return file.findDeclByPath(name)
}

func exprToDecl(e ast.Expr, file *PackageFile) *Decl {
	expr := NewDeclVar("tmp", nil, e, -1, file).InferType()
	if expr == nil {
		return nil
	}

	name := typePath(expr)
	var typedecl *Decl
	if name == "0" {
		typedecl = NewDeclVar("tmp", expr, nil, -1, file)
	} else {
		typedecl = file.findDeclByPath(name)
	}
	return typedecl
}

// return expr and true if it's a type or false if it's a value
func inferType(v ast.Expr, index int, file *PackageFile) (ast.Expr, bool) {
	//topLevel := file.ctx
	//ty := reflect.Typeof(v)
	//fmt.Println(ty)
	switch t := v.(type) {
	case *ast.CompositeLit:
		return t.Type, true
	case *ast.Ident:
		if d := file.findDecl(t.Name()); d != nil {
			// we don't check for DECL_MODULE here, because module itself
			// isn't a type, in a type context it always will be used together
			// with	SelectorExpr like: os.Error, ast.TypeSpec, etc.
			// and SelectorExpr ignores type bool.
			return d.InferType(), d.Class == DECL_TYPE
		}
		return t, true // probably a builtin
	case *ast.UnaryExpr:
		switch t.Op {
		case token.AND:
			// & makes sense only with values, don't even check for type
			it, _ := inferType(t.X, -1, file)
			if it == nil {
				break
			}

			e := new(ast.StarExpr)
			e.X = it
			return e, false
		case token.ARROW:
			// <- makes sense only with values
			it, _ := inferType(t.X, -1, file)
			if it == nil {
				break
			}
			switch index {
			case -1, 0:
				return it.(*ast.ChanType).Value, false
			case 1:
				// technically it's a value, but in case of index == 1
				// it is always the last infer operation
				return ast.NewIdent("bool"), false
			}
		}
	case *ast.IndexExpr:
		// something[another] always returns a value and it works on a value too
		it, _ := inferType(t.X, -1, file)
		if it == nil {
			break
		}
		switch t := it.(type) {
		case *ast.ArrayType:
			return t.Elt, false
		case *ast.MapType:
			switch index {
			case -1, 0:
				return t.Value, false
			case 1:
				return ast.NewIdent("bool"), false
			}
		}
	case *ast.StarExpr:
		it, isType := inferType(t.X, -1, file)
		if it == nil {
			break
		}
		if isType {
			// it it's a type, add * modifier, make it a 'pointer of' type
			e := new(ast.StarExpr)
			e.X = it
			return e, true
		} else if s, ok := it.(*ast.StarExpr); ok {
			// if it's a pointer value, dereference pointer
			return s.X, false
		}
	case *ast.CallExpr:
		ty := checkForBuiltinFuncs(t)
		if ty != nil {
			// all built-in functions return a value
			return ty, false
		}

		it, _ := inferType(t.Fun, -1, file)
		if it == nil {
			break
		}
		switch t := it.(type) {
		case *ast.FuncType:
			// in case if <here>() is a function type variable, we're making a 
			// func call, resulting expr is always a value
			return funcReturnType(t, index), false
		default:
			// otherwise it's a type cast, and the result is a value too
			return t, false
		}
	case *ast.ParenExpr:
		it, isType := inferType(t.X, -1, file)
		if it == nil {
			break
		}
		return it, isType
	case *ast.SelectorExpr:
		it, _ := inferType(t.X, -1, file)
		if it == nil {
			break
		}

		if d := typeToDecl(it, file); d != nil {
			c := d.FindChildAndInEmbedded(t.Sel.Name())
			if c != nil {
				if c.Class == DECL_TYPE {
					return t, true
				} else {
					return c.InferType(), false
				}
			}
		}
	case *ast.FuncLit:
		// it's a value, but I think most likely we don't even care, cause we can only
		// call it, and CallExpr uses the type itself to figure out
		return t.Type, false
	case *ast.TypeAssertExpr:
		if t.Type == nil {
			return inferType(t.X, -1, file)
		}
		switch index {
		case -1, 0:
			// converting a value to a different type, but return thing is a value
			return t.Type, false
		case 1:
			return ast.NewIdent("bool"), false
		}
	case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType:
		return t, true
	default:
		_ = reflect.Typeof(v)
		//fmt.Println(ty)
	}
	return nil, false
}

func (d *Decl) InferType() ast.Expr {
	switch d.Class {
	case DECL_TYPE:
		return ast.NewIdent(d.Name)
	case DECL_MODULE:
		name := d.File.localModuleName(d.Name)
		if name == "" {
			return nil
		}
		return ast.NewIdent(name)
	}

	// shortcut
	if d.Type != nil && d.Value == nil {
		return d.Type
	}

	d.Type, _ = inferType(d.Value, d.ValueIndex, d.File)
	return d.Type
}

func (d *Decl) FindChild(name string) *Decl {
	for _, c := range d.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (d *Decl) FindChildNum(name string) int {
	for i, c := range d.Children {
		if c.Name == name {
			return i
		}
	}
	return -1
}

func (d *Decl) FindChildAndInEmbedded(name string) *Decl {
	c := d.FindChild(name)
	if c == nil {
		for _, e := range d.Embedded {
			typedecl := typeToDecl(e, d.File)
			c = typedecl.FindChildAndInEmbedded(name)
			if c != nil {
				break
			}
		}
	}
	return c
}
