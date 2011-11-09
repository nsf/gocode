package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"text/scanner"
)

//-------------------------------------------------------------------------
// PackageFileCache
//
// Structure that represents a cache for an imported pacakge. In other words
// these are the contents of an archive (*.a) file.
//-------------------------------------------------------------------------

type PackageFileCache struct {
	name     string // file name
	mtime    int64
	defalias string

	scope  *Scope
	main   *Decl // package declaration
	others map[string]*Decl
}

func NewPackageFileCache(name string) *PackageFileCache {
	m := new(PackageFileCache)
	m.name = name
	m.mtime = 0
	m.defalias = ""
	return m
}

// Creates a cache that stays in cache forever. Useful for built-in packages.
func NewPackageFileCacheForever(name, defalias string) *PackageFileCache {
	m := new(PackageFileCache)
	m.name = name
	m.mtime = -1
	m.defalias = defalias
	return m
}

func (m *PackageFileCache) findFile() string {
	if fileExists(m.name) {
		return m.name
	}

	n := len(m.name)
	filename := m.name[:n-1] + "6"
	if fileExists(filename) {
		return filename
	}

	filename = m.name[:n-1] + "8"
	if fileExists(filename) {
		return filename
	}

	filename = m.name[:n-1] + "5"
	if fileExists(filename) {
		return filename
	}
	return m.name
}

func (m *PackageFileCache) updateCache() {
	if m.mtime == -1 {
		return
	}
	fname := m.findFile()
	stat, err := os.Stat(fname)
	if err != nil {
		return
	}

	if m.mtime != stat.Mtime_ns {
		m.mtime = stat.Mtime_ns

		data, err := ioutil.ReadFile(fname)
		if err != nil {
			return
		}
		m.processPackageData(data)
	}
}

func (m *PackageFileCache) processPackageData(data []byte) {
	m.scope = NewScope(universeScope)

	// find import section
	i := bytes.Index(data, []byte{'\n', '$', '$'})
	if i == -1 {
		panic("Can't find the import section in the package file")
	}
	data = data[i+len("\n$$"):]

	// find the beginning of the package clause
	i = bytes.Index(data, []byte{'p', 'a', 'c', 'k', 'a', 'g', 'e'})
	if i == -1 {
		panic("Can't find the package clause")
	}
	data = data[i:]

	buf := bytes.NewBuffer(data)
	var p gcParser
	p.init(buf, m)
	// and the built-in "unsafe" package to the pathToAlias map
	p.pathToAlias["unsafe"] = "unsafe"
	// create map for other packages
	m.others = make(map[string]*Decl)
	p.parseExport(func(pkg string, decl ast.Decl) {
		if pkg == "" {
			// main package
			if m.main == nil {
				m.main = NewDecl(m.name, DECL_PACKAGE, nil)
			}
			addAstDeclToPackage(m.main, decl, m.scope)
		} else {
			// others
			if _, ok := m.others[pkg]; !ok {
				m.others[pkg] = NewDecl(pkg, DECL_PACKAGE, nil)
			}
			addAstDeclToPackage(m.others[pkg], decl, m.scope)
		}
	})

	// hack, add ourselves to the package scope
	m.addPackageToScope(m.defalias, m.name)

	// WTF is that? :D
	for key, value := range m.scope.entities {
		pkg, ok := m.others[value.Name]
		if !ok && value.Name == m.name {
			pkg = m.main
		}
		m.scope.replaceDecl(key, pkg)
	}
}

func (m *PackageFileCache) addPackageToScope(alias, realname string) {
	d := NewDecl(realname, DECL_PACKAGE, nil)
	m.scope.addDecl(alias, d)
}

func addAstDeclToPackage(pkg *Decl, decl ast.Decl, scope *Scope) {
	foreachDecl(decl, func(data *foreachDeclStruct) {
		class := astDeclClass(data.decl)
		for i, name := range data.names {
			typ, v, vi := data.typeValueIndex(i, DECL_FOREIGN)

			d := NewDecl2(name.Name, class, DECL_FOREIGN, typ, v, vi, scope)
			if d == nil {
				return
			}

			if !name.IsExported() && d.Class != DECL_TYPE {
				return
			}

			methodof := MethodOf(data.decl)
			if methodof != "" {
				decl := pkg.FindChild(methodof)
				if decl != nil {
					decl.AddChild(d)
				} else {
					decl = NewDecl(methodof, DECL_METHODS_STUB, scope)
					decl.AddChild(d)
					pkg.AddChild(decl)
				}
			} else {
				decl := pkg.FindChild(d.Name)
				if decl != nil {
					decl.ExpandOrReplace(d)
				} else {
					pkg.AddChild(d)
				}
			}
		}
	})
}

//-------------------------------------------------------------------------
// gcParser
//
// The following part of the code may contain portions of the code from the Go
// standard library, which tells me to retain their copyright notice:
//
// Copyright (c) 2009 The Go Authors. All rights reserved.
// 
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
// 
//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
// 
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//-------------------------------------------------------------------------

type gcParser struct {
	scanner     scanner.Scanner
	tok         int
	lit         string
	pathToAlias map[string]string
	beautify    bool
	pfc         *PackageFileCache
}

func (p *gcParser) init(src io.Reader, pfc *PackageFileCache) {
	p.scanner.Init(src)
	p.scanner.Error = func(_ *scanner.Scanner, msg string) { p.error(msg) }
	p.scanner.Mode = scanner.ScanIdents | scanner.ScanInts | scanner.ScanStrings |
		scanner.ScanComments | scanner.SkipComments
	p.scanner.Whitespace = 1<<'\t' | 1<<' ' | 1<<'\r' | 1<<'\v' | 1<<'\f'
	p.scanner.Filename = "package.go"
	p.next()
	p.pathToAlias = make(map[string]string)
	p.pfc = pfc
}

func (p *gcParser) next() {
	p.tok = p.scanner.Scan()
	switch p.tok {
	case scanner.Ident, scanner.Int, scanner.String:
		p.lit = p.scanner.TokenText()
	default:
		p.lit = ""
	}
}

func (p *gcParser) error(msg string) {
	panic(errors.New(msg))
}

func (p *gcParser) errorf(format string, args ...interface{}) {
	p.error(fmt.Sprintf(format, args...))
}

func (p *gcParser) expect(tok int) string {
	lit := p.lit
	if p.tok != tok {
		p.errorf("expected %s, got %s (%q)", scanner.TokenString(tok),
			scanner.TokenString(p.tok), lit)
	}
	p.next()
	return lit
}

func (p *gcParser) expectKeyword(keyword string) {
	lit := p.expect(scanner.Ident)
	if lit != keyword {
		p.errorf("expected keyword: %s, got: %q", keyword, lit)
	}
}

func (p *gcParser) expectSpecial(what string) {
	i := 0
	for i < len(what) {
		if p.tok != int(what[i]) {
			break
		}

		nc := p.scanner.Peek()
		if i != len(what)-1 && nc <= ' ' {
			break
		}

		p.next()
		i++
	}

	if i < len(what) {
		p.errorf("expected: %q, got: %q", what, what[0:i])
	}
}

// dotIdentifier = ( ident | '·' ) { ident | int | '·' } .
// we're doing lexer job here, kind of
func (p *gcParser) parseDotIdent() string {
	ident := ""
	sep := 'x'
	for (p.tok == scanner.Ident || p.tok == scanner.Int || p.tok == '·') && sep > ' ' {
		ident += p.lit
		if p.tok == '·' {
			ident += "·"
		}
		sep = p.scanner.Peek()
		p.next()
	}
	return ident
}

// ImportPath = string_lit .
// quoted name of the path, but we return it as an identifier, taking an alias
// from 'pathToAlias' map, it is filled by import statements
func (p *gcParser) parsePackage() *ast.Ident {
	path, err := strconv.Unquote(p.expect(scanner.String))
	if err != nil {
		panic(err)
	}

	return ast.NewIdent(path)
}

// ExportedName = "@" ImportPath "." dotIdentifier .
func (p *gcParser) parseExportedName() *ast.SelectorExpr {
	p.expect('@')
	pkg := p.parsePackage()
	if p.beautify {
		if pkg.Name == "" {
			pkg.Name = p.pfc.defalias
		} else {
			pkg.Name = p.pathToAlias[pkg.Name]
		}
	}
	p.expect('.')
	name := ast.NewIdent(p.parseDotIdent())
	return &ast.SelectorExpr{X: pkg, Sel: name}
}

// Name = identifier | "?" .
func (p *gcParser) parseName() string {
	switch p.tok {
	case scanner.Ident:
		name := p.lit
		p.next()
		return name
	case '?':
		p.next()
		return "?"
	}
	p.error("name expected")
	return ""
}

// Field = Name Type [ string_lit ] .
func (p *gcParser) parseField() *ast.Field {
	var tag string
	name := p.parseName()
	typ := p.parseType()
	if p.tok == scanner.String {
		tag = p.expect(scanner.String)
	}

	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  typ,
		Tag:   &ast.BasicLit{Kind: token.STRING, Value: tag},
	}
}

// Parameter = ( identifier | "?" ) [ "..." ] Type [ string_lit ] .
func (p *gcParser) parseParameter() *ast.Field {
	// name
	name := p.parseName()

	// type
	var typ ast.Expr
	if p.tok == '.' {
		p.expectSpecial("...")
		typ = &ast.Ellipsis{Elt: p.parseType()}
	} else {
		typ = p.parseType()
	}

	var tag string
	if p.tok == scanner.String {
		tag = p.expect(scanner.String)
	}

	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  typ,
		Tag:   &ast.BasicLit{Kind: token.STRING, Value: tag},
	}
}

// Parameters = "(" [ ParameterList ] ")" .
// ParameterList = { Parameter "," } Parameter .
func (p *gcParser) parseParameters() *ast.FieldList {
	var flds []*ast.Field
	parseParameter := func() {
		par := p.parseParameter()
		flds = append(flds, par)
	}

	p.expect('(')
	if p.tok != ')' {
		parseParameter()
		for p.tok == ',' {
			p.next()
			parseParameter()
		}
	}
	p.expect(')')

	if flds == nil {
		return nil
	}
	return &ast.FieldList{List: flds}
}

// Signature = Parameters [ Result ] .
// Result = Type | Parameters .
func (p *gcParser) parseSignature() *ast.FuncType {
	var params *ast.FieldList
	var results *ast.FieldList

	params = p.parseParameters()
	switch p.tok {
	case scanner.Ident, '[', '*', '<', '@':
		fld := &ast.Field{Type: p.parseType()}
		results = &ast.FieldList{List: []*ast.Field{fld}}
	case '(':
		results = p.parseParameters()
	}
	return &ast.FuncType{Params: params, Results: results}
}

// MethodSpec = ( identifier | ExportedName ) Signature .
func (p *gcParser) parseMethodSpec() *ast.Field {
	var name string

	if p.tok == scanner.Ident {
		name = p.expect(scanner.Ident)
	} else {
		// here we'll skip package crap and just use the name
		p.expect('@')
		p.parsePackage()
		p.expect('.')
		name = p.parseDotIdent()
	}
	typ := p.parseSignature()
	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  typ,
	}
}

// int_lit = [ "-" | "+" ] { "0" ... "9" } .
func (p *gcParser) parseInt() {
	switch p.tok {
	case '-', '+':
		p.next()
	}
	p.expect(scanner.Int)
}

// number = int_lit [ "p" int_lit ] .
func (p *gcParser) parseNumber() {
	p.parseInt()
	if p.lit == "p" {
		p.next()
		p.parseInt()
	}
}

//-------------------------------------------------------------------------------
// gcParser.Types
//-------------------------------------------------------------------------------

// InterfaceType = "interface" "{" [ MethodList ] "}" .
// MethodList = MethodSpec { ";" MethodSpec } .
func (p *gcParser) parseInterfaceType() ast.Expr {
	var methods []*ast.Field
	parseMethod := func() {
		meth := p.parseMethodSpec()
		methods = append(methods, meth)
	}

	p.expectKeyword("interface")
	p.expect('{')
	if p.tok != '}' {
		parseMethod()
		for p.tok == ';' {
			p.next()
			parseMethod()
		}
	}
	p.expect('}')
	return &ast.InterfaceType{Methods: &ast.FieldList{List: methods}}
}

// StructType = "struct" "{" [ FieldList ] "}" .
// FieldList = Field { ";" Field } .
func (p *gcParser) parseStrucType() ast.Expr {
	var fields []*ast.Field
	parseField := func() {
		fld := p.parseField()
		fields = append(fields, fld)
	}

	p.expectKeyword("struct")
	p.expect('{')
	if p.tok != '}' {
		parseField()
		for p.tok == ';' {
			p.next()
			parseField()
		}
	}
	p.expect('}')
	return &ast.StructType{Fields: &ast.FieldList{List: fields}}
}

// MapType = "map" "[" Type "]" Type .
func (p *gcParser) parseMapType() ast.Expr {
	p.expectKeyword("map")
	p.expect('[')
	key := p.parseType()
	p.expect(']')
	elt := p.parseType()
	return &ast.MapType{Key: key, Value: elt}
}

// ChanType = ( "chan" [ "<-" ] | "<-" "chan" ) Type .
func (p *gcParser) parseChanType() ast.Expr {
	dir := ast.SEND | ast.RECV
	if p.tok == scanner.Ident {
		p.expectKeyword("chan")
		if p.tok == '<' {
			p.expectSpecial("<-")
			dir = ast.SEND
		}
	} else {
		p.expectSpecial("<-")
		p.expectKeyword("chan")
		dir = ast.RECV
	}

	elt := p.parseType()
	return &ast.ChanType{Dir: dir, Value: elt}
}

// ArrayOrSliceType = ArrayType | SliceType .
// ArrayType = "[" int_lit "]" Type .
// SliceType = "[" "]" Type .
func (p *gcParser) parseArrayOrSliceType() ast.Expr {
	p.expect('[')
	if p.tok == ']' {
		// SliceType
		p.next() // skip ']'
		return &ast.ArrayType{Len: nil, Elt: p.parseType()}
	}

	// ArrayType
	lit := p.expect(scanner.Int)
	p.expect(']')
	return &ast.ArrayType{
		Len: &ast.BasicLit{Kind: token.INT, Value: lit},
		Elt: p.parseType(),
	}
}

// Type =
//	BasicType | TypeName | ArrayType | SliceType | StructType |
//      PointerType | FuncType | InterfaceType | MapType | ChanType |
//      "(" Type ")" .
// BasicType = ident .
// TypeName = ExportedName .
// SliceType = "[" "]" Type .
// PointerType = "*" Type .
// FuncType = "func" Signature .
func (p *gcParser) parseType() ast.Expr {
	switch p.tok {
	case scanner.Ident:
		switch p.lit {
		case "struct":
			return p.parseStrucType()
		case "func":
			p.next()
			return p.parseSignature()
		case "interface":
			return p.parseInterfaceType()
		case "map":
			return p.parseMapType()
		case "chan":
			return p.parseChanType()
		default:
			lit := p.lit
			p.next()
			return ast.NewIdent(lit)
		}
	case '@':
		return p.parseExportedName()
	case '[':
		return p.parseArrayOrSliceType()
	case '*':
		p.next()
		return &ast.StarExpr{X: p.parseType()}
	case '<':
		return p.parseChanType()
	case '(':
		p.next()
		typ := p.parseType()
		p.expect(')')
		return typ
	}
	p.errorf("unexpected token: %s", scanner.TokenString(p.tok))
	return nil
}

//-------------------------------------------------------------------------------
// gcParser.Declarations
//-------------------------------------------------------------------------------

// ImportDecl = "import" identifier string_lit .
func (p *gcParser) parseImportDecl() {
	p.expectKeyword("import")
	alias := p.expect(scanner.Ident)
	path := p.parsePackage()
	p.pathToAlias[path.Name] = alias
	p.pfc.addPackageToScope(alias, path.Name)
}

// ConstDecl   = "const" ExportedName [ Type ] "=" Literal .
// Literal     = bool_lit | int_lit | float_lit | complex_lit | string_lit .
// bool_lit    = "true" | "false" .
// complex_lit = "(" float_lit "+" float_lit ")" .
// string_lit  = `"` { unicode_char } `"` .
func (p *gcParser) parseConstDecl() (string, *ast.GenDecl) {
	// TODO: do we really need actual const value? gocode doesn't use this
	p.expectKeyword("const")
	name := p.parseExportedName()
	p.beautify = true

	var typ ast.Expr
	if p.tok != '=' {
		typ = p.parseType()
	}

	p.expect('=')

	// skip the value
	switch p.tok {
	case scanner.Ident:
		// must be bool, true or false
		p.next()
	case '-', '+', scanner.Int:
		// number
		p.parseNumber()
	case '(':
		// complex_lit
		p.next() // skip '('
		p.parseNumber()
		p.expect('+')
		p.parseNumber()
		p.expect(')')
	case scanner.String:
		p.next()
	default:
		p.error("expected literal")
	}

	return name.X.(*ast.Ident).Name, &ast.GenDecl{
		Tok: token.CONST,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names:  []*ast.Ident{name.Sel},
				Type:   typ,
				Values: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "0"}},
			},
		},
	}
}

// TypeDecl = "type" ExportedName Type .
func (p *gcParser) parseTypeDecl() (string, *ast.GenDecl) {
	p.expectKeyword("type")
	name := p.parseExportedName()
	p.beautify = true
	typ := p.parseType()
	return name.X.(*ast.Ident).Name, &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: name.Sel,
				Type: typ,
			},
		},
	}
}

// VarDecl = "var" ExportedName Type .
func (p *gcParser) parseVarDecl() (string, *ast.GenDecl) {
	p.expectKeyword("var")
	name := p.parseExportedName()
	p.beautify = true
	typ := p.parseType()
	return name.X.(*ast.Ident).Name, &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names: []*ast.Ident{name.Sel},
				Type:  typ,
			},
		},
	}
}

// FuncBody = "{" ... "}" .
func (p *gcParser) parseFuncBody() {
	p.expect('{')
	for i := 1; i > 0; p.next() {
		switch p.tok {
		case '{':
			i++
		case '}':
			i--
		}
	}
}

// FuncDecl = "func" ExportedName Signature [ FuncBody ] .
func (p *gcParser) parseFuncDecl() (string, *ast.FuncDecl) {
	// "func" was already consumed by lookahead
	name := p.parseExportedName()
	p.beautify = true
	typ := p.parseSignature()
	if p.tok == '{' {
		p.parseFuncBody()
	}
	return name.X.(*ast.Ident).Name, &ast.FuncDecl{
		Name: name.Sel,
		Type: typ,
	}
}

func stripMethodReceiver(recv *ast.FieldList) string {
	var sel *ast.SelectorExpr

	// find selector expression
	typ := recv.List[0].Type
	switch t := typ.(type) {
	case *ast.StarExpr:
		sel = t.X.(*ast.SelectorExpr)
	case *ast.SelectorExpr:
		sel = t
	}

	// extract package path
	pkg := sel.X.(*ast.Ident).Name

	// write back stripped type
	switch t := typ.(type) {
	case *ast.StarExpr:
		t.X = sel.Sel
	case *ast.SelectorExpr:
		recv.List[0].Type = sel.Sel
	}

	return pkg
}

// MethodDecl = "func" Receiver identifier Signature .
// Receiver = "(" ( identifier | "?" ) [ "*" ] ExportedName ")" [ FuncBody ] .
func (p *gcParser) parseMethodDecl() (string, *ast.FuncDecl) {
	recv := p.parseParameters()
	p.beautify = true
	pkg := stripMethodReceiver(recv)
	name := p.expect(scanner.Ident)
	typ := p.parseSignature()
	if p.tok == '{' {
		p.parseFuncBody()
	}
	return pkg, &ast.FuncDecl{
		Recv: recv,
		Name: ast.NewIdent(name),
		Type: typ,
	}
}

// Decl = [ ImportDecl | ConstDecl | TypeDecl | VarDecl | FuncDecl | MethodDecl ] "\n" .
func (p *gcParser) parseDecl() (pkg string, decl ast.Decl) {
	switch p.lit {
	case "import":
		p.parseImportDecl()
	case "const":
		pkg, decl = p.parseConstDecl()
	case "type":
		pkg, decl = p.parseTypeDecl()
	case "var":
		pkg, decl = p.parseVarDecl()
	case "func":
		p.next()
		if p.tok == '(' {
			pkg, decl = p.parseMethodDecl()
		} else {
			pkg, decl = p.parseFuncDecl()
		}
	}
	p.expect('\n')
	return
}

// Export = PackageClause { Decl } "$$" .
// PackageClause = "package" identifier [ "safe" ] "\n" .
func (p *gcParser) parseExport(callback func(string, ast.Decl)) {
	p.expectKeyword("package")
	p.pfc.defalias = p.expect(scanner.Ident)
	if p.tok != '\n' {
		p.expectKeyword("safe")
	}
	p.expect('\n')

	for p.tok != '$' && p.tok != scanner.EOF {
		p.beautify = false
		pkg, decl := p.parseDecl()
		if decl != nil {
			callback(pkg, decl)
		}
	}
}
//-------------------------------------------------------------------------
// PackageCache
//-------------------------------------------------------------------------

type PackageCache map[string]*PackageFileCache

func NewPackageCache() PackageCache {
	m := make(PackageCache)

	// add built-in "unsafe" package
	m.addBuiltinUnsafePackage()

	return m
}

// Function fills 'ps' set with packages from 'packages' import information.
// In case if package is not in the cache, it creates one and adds one to the cache.
func (c PackageCache) AppendPackages(ps map[string]*PackageFileCache, pkgs PackageImports) {
	for _, m := range pkgs {
		if _, ok := ps[m.Path]; ok {
			continue
		}

		if mod, ok := c[m.Path]; ok {
			ps[m.Path] = mod
		} else {
			mod = NewPackageFileCache(m.Path)
			ps[m.Path] = mod
			c[m.Path] = mod
		}
	}
}

var builtinUnsafePackage = []byte(`
import
$$
package unsafe 
	type @"".Pointer uintptr
	func @"".Offsetof (? any) uintptr
	func @"".Sizeof (? any) uintptr
	func @"".Alignof (? any) uintptr
	func @"".Typeof (i interface { }) interface { }
	func @"".Reflect (i interface { }) (typ interface { }, addr @"".Pointer)
	func @"".Unreflect (typ interface { }, addr @"".Pointer) interface { }
	func @"".New (typ interface { }) @"".Pointer
	func @"".NewArray (typ interface { }, n int) @"".Pointer

$$
`)

func (c PackageCache) addBuiltinUnsafePackage() {
	name, _ := findGlobalFile("unsafe")
	pkg := NewPackageFileCacheForever(name, "unsafe")
	pkg.processPackageData(builtinUnsafePackage)
	c[name] = pkg
}
