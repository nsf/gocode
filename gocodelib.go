package main

import (
	"fmt"
	"bytes"
	"go/parser"
	"go/ast"
	"go/token"
	"go/scanner"
	"strings"
	"io/ioutil"
	"hash/crc32"
	"reflect"
	"path"
	"sort"
	"io"
	"os"
)

type TokPos struct {
	Tok token.Token
	Pos token.Position
}

type TokCollection struct {
	tokens []TokPos
}

func (self *TokCollection) appendToken(pos token.Position, tok token.Token) {
	if self.tokens == nil {
		self.tokens = make([]TokPos, 0, 4)
	}

	if cap(self.tokens) < len(self.tokens)+1 {
		newcap := cap(self.tokens) * 2
		if newcap == 0 {
			newcap = 4
		}

		s := make([]TokPos, len(self.tokens), newcap)
		copy(s, self.tokens)
		self.tokens = s
	}

	i := len(self.tokens)
	self.tokens = self.tokens[0:i+1]
	self.tokens[i] = TokPos{tok, pos}
}

func (self *TokCollection) next(s *scanner.Scanner) bool {
	pos, tok, _ := s.Scan()
	if tok == token.EOF {
		return false
	}

	self.appendToken(pos, tok)
	return true
}

func (self *TokCollection) findDeclBeg(pos int) int {
	lowest := 0
	lowpos := -1
	lowi := -1
	cur := 0
	for i := pos; i >= 0; i-- {
		switch self.tokens[i].Tok {
		case token.RBRACE:
			cur++
		case token.LBRACE:
			cur--
		}

		if cur < lowest {
			lowest = cur
			lowpos = self.tokens[i].Pos.Offset
			lowi = i
		}
	}

	for i := lowi; i >= 0; i-- {
		if self.tokens[i].Tok == token.SEMICOLON {
			lowpos = self.tokens[i+1].Pos.Offset
			break
		}
	}

	return lowpos
}

func (self *TokCollection) findDeclEnd(pos int) int {
	highest := 0
	highpos := -1
	cur := 0

	if self.tokens[pos].Tok == token.LBRACE {
		pos++
	}

	for i := pos; i < len(self.tokens); i++ {
		switch self.tokens[i].Tok {
		case token.RBRACE:
			cur++
		case token.LBRACE:
			cur--
		}

		if cur > highest {
			highest = cur
			highpos = self.tokens[i].Pos.Offset
		}
	}

	return highpos
}

func (self *TokCollection) findOutermostScope(cursor int) (int, int) {
	pos := 0

	for i, t := range self.tokens {
		if cursor <= t.Pos.Offset {
			break
		}
		pos = i
	}

	return self.findDeclBeg(pos), self.findDeclEnd(pos)
}

// return new cursor position, file without ripped part and the ripped part itself
// variants:
//   new-cursor, file-without-ripped-part, ripped-part
//   old-cursor, file, nil
func (self *TokCollection) ripOffDecl(file []byte, cursor int) (int, []byte, []byte) {
	s := new(scanner.Scanner)
	s.Init("", file, nil, scanner.ScanComments | scanner.InsertSemis)
	for self.next(s) {
	}

	beg, end := self.findOutermostScope(cursor)
	if beg == -1 || end == -1 {
		return cursor, file, nil
	}

	ripped := make([]byte, end + 1 - beg)
	copy(ripped, file[beg:end+1])

	newfile := make([]byte, len(file) - len(ripped))
	copy(newfile, file[0:beg])
	copy(newfile[beg:], file[end+1:])

	return cursor - beg, newfile, ripped
}

// TODO: probably change hand-written string literals processing to a
// "scanner"-based one

func skipSpaces(i int, s string) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func skipToSpace(i int, s string) int {
	for i < len(s) && s[i] != ' ' && s[i] != '\t' {
		i++
	}
	return i
}

// convert package name to a nice ident, e.g.: "go/ast" -> "ast"
func identifyPackage(s string) string {
	i := len(s)-1

	// 'i > 0' is correct here, because we should never see '/' at the
	// beginning of the name anyway
	for ; i > 0; i-- {
		if s[i] == '/' {
			break
		}
	}
	if s[i] == '/' {
		return s[i+1:]
	}
	return s
}

func extractPackage(i int, s string) (string, string) {
	pkg := ""

	b := i // first '"'
	i++

	for i < len(s) && s[i] != '"' {
		i++
	}

	if i == len(s) {
		return s, pkg
	}

	e := i // second '"'
	if b+1 != e {
		// wow, we actually have something here
		pkg = s[b+1:e]
	}

	i += 2 // skip to a first symbol after dot
	s = s[0:b] + s[i:] // strip package clause completely

	return s, pkg
}

// returns modified 's' with package stripped from the method and the package name
func extractPackageFromMethod(i int, s string) (string, string) {
	pkg := ""
	for {
		for i < len(s) && s[i] != ')' && s[i] != '"' {
			i++
		}

		if s[i] == ')' || i == len(s) {
			return s, pkg
		}

		b := i // first '"'
		i++

		for i < len(s) && s[i] != ')' && s[i] != '"' {
			i++
		}

		if s[i] == ')' || i == len(s) {
			return s, pkg
		}

		e := i // second '"'
		if b+1 != e {
			// wow, we actually have something here
			pkg = s[b+1:e]
		}

		i += 2 // skip to a first symbol after dot
		s = s[0:b] + s[i:] // strip package clause completely

		i = b
	}
	panic("unreachable")
	return "", ""
}

func (self *AutoCompleteContext) expandPackages(s, curpkg string) string {
	i := 0
	for {
		pkg := ""
		for i < len(s) && s[i] != '"' && s[i] != '=' {
			i++
		}

		if i == len(s) || s[i] == '=' {
			return s
		}

		b := i // first '"'
		i++

		for i < len(s) && !(s[i] == '"' && s[i-1] != '\\') && s[i] != '=' {
			i++
		}

		if i == len(s) || s[i] == '=' {
			return s
		}

		e := i // second '"'
		if s[b-1] == ':' {
			// special case, struct attribute literal, just remove ':'
			s = s[0:b-1] + s[b:]
			i = e
		} else if b+1 != e {
			// wow, we actually have something here
			pkgalias := identifyPackage(s[b+1:e])
			pkg = self.genForeignPackageAlias(pkgalias, s[b+1:e])
			i++ // skip to a first symbol after second '"'
			s = s[0:b] + pkg + s[i:] // strip package clause completely
			i = b
		} else {
			pkgalias := identifyPackage(curpkg)
			pkg = self.genForeignPackageAlias(pkgalias, curpkg)
			i++
			s = s[0:b] + pkg + s[i:]
			i = b
		}

	}
	panic("unreachable")
	return ""
}

func preprocessConstDecl(s string) string {
	i := strings.Index(s, "=")
	if i == -1 {
		return s
	}

	for i < len(s) && !(s[i] >= '0' && s[i] <= '9') && s[i] != '"' && s[i] != '\'' {
		i++
	}

	if i == len(s) || s[i] == '"' || s[i] == '\'' {
		return s
	}

	// ok, we have a digit!
	b := i
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == 'p' || s[i] == '-' || s[i] == '+') {
		i++
	}
	e := i

	return s[0:b] + "0" + s[e:]
}

// feed one definition line from .a file here
// returns:
// 1. a go/parser parsable string representing one Go declaration
// 2. and a package name this declaration belongs to
func (self *AutoCompleteContext) processExport(s, curpkg string) (string, string) {
	i := 0
	pkg := ""

	// skip to a decl type: (type | func | const | var | import)
	i = skipSpaces(i, s)
	if i == len(s) {
		return "", pkg
	}
	b := i
	i = skipToSpace(i, s)
	if i == len(s) {
		return "", pkg
	}
	e := i

	switch s[b:e] {
	case "import":
		// skip import decls, we don't need them
		return "", pkg
	case "const":
		s = preprocessConstDecl(s)
	}
	i++ // skip space after a decl type

	// extract a package this decl belongs to
	switch s[i] {
	case '(':
		s, pkg = extractPackageFromMethod(i, s)
	case '"':
		s, pkg = extractPackage(i, s)
	}

	// make everything parser friendly
	s = strings.Replace(s, "?", "", -1)
	s = self.expandPackages(s, curpkg)

	// skip system functions (Init, etc.)
	i = strings.Index(s, "·")
	if i != -1 {
		return "", ""
	}

	return s, pkg
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
				names[i] = name.Name()
			}
		case token.TYPE:
			t := t.Specs[0].(*ast.TypeSpec)
			names = make([]string, 1)
			names[0] = t.Name.Name()
		case token.VAR:
			v := t.Specs[0].(*ast.ValueSpec)
			names = make([]string, len(v.Names))
			for i, name := range v.Names {
				names[i] = name.Name()
			}
		}
	case *ast.FuncDecl:
		names = make([]string, 1)
		names[0] = t.Name.Name()
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

const builtinUnsafePackage = `
import
$$
package unsafe 
	type "".Pointer *any
	func "".Offsetof (? any) int
	func "".Sizeof (? any) int
	func "".Alignof (? any) int
	func "".Typeof (i interface { }) interface { }
	func "".Reflect (i interface { }) (typ interface { }, addr "".Pointer)
	func "".Unreflect (typ interface { }, addr "".Pointer) interface { }
	func "".New (typ interface { }) "".Pointer
	func "".NewArray (typ interface { }, n int) "".Pointer

$$
`

func (self *AutoCompleteContext) addBuiltinUnsafe() {
	self.processPackageData("unsafe", builtinUnsafePackage)
	self.cache[findGlobalFile("unsafe")] = NewModuleCacheForever("unsafe")
}

func (self *PackageFile) processPackage(filename, uniquename, pkgname string) {
	if pkgname == "_" {
		// ignore packages imported only for side effects
		return
	}
	// TODO: deal with packages imported in the current namespace
	if uniquename[0] == '.' {
		// for local packages use full path as an unique name
		uniquename = filename
	}

	if m, ok := self.ctx.cache[filename]; ok {
		m.updateCache()
		if pkgname == "" {
			pkgname = m.defalias
		}
		self.addPackageAlias(pkgname, uniquename)
		return
	}

	self.ctx.cache[filename] = NewModuleCache(filename, uniquename, self.ctx)
	m := self.ctx.cache[filename]

	if pkgname == "" {
		pkgname = m.defalias
	}
	self.addPackageAlias(pkgname, uniquename)
}

func (self *AutoCompleteContext) processPackageData(uniquename, s string) string {
	i := strings.Index(s, "import\n$$\n")
	if i == -1 {
		panic("Can't find the import section in the archive file")
	}
	s = s[i+len("import\n$$\n"):]
	i = strings.Index(s, "$$\n")
	if i == -1 {
		panic("Can't find the end of the import section in the archive file")
	}
	s = s[0:i] // leave only import section

	i = strings.Index(s, "\n")
	if i == -1 {
		panic("Wrong file")
	}

	defpkgname := s[len("package "):i-1]
	self.debugLog("parsing package '%s'...\n", defpkgname)
	s = s[i+1:]

	internalPackages := make(map[string]*bytes.Buffer)
	for {
		// for each line
		i := strings.Index(s, "\n")
		if i == -1 {
			break
		}
		decl := strings.TrimSpace(s[0:i])
		if len(decl) == 0 {
			s = s[i+1:]
			continue
		}
		decl2, pkg := self.processExport(decl, uniquename)
		if len(decl2) == 0 {
			s = s[i+1:]
			continue
		}

		if pkg == "" {
			// local package, use ours name
			pkg = uniquename
		}

		buf := internalPackages[pkg]
		if buf == nil {
			buf = bytes.NewBuffer(make([]byte, 0, 4096))
			internalPackages[pkg] = buf
		}
		buf.WriteString(decl2)
		buf.WriteString("\n")
		s = s[i+1:]
	}
	for key, value := range internalPackages {
		decls, err := parser.ParseDeclList("", value.Bytes(), nil)
		if err != nil {
			panic(fmt.Sprintf("failure in:\n%s\n%s\n", value, err.String()))
		} else {
			f := new(ast.File) // fake file
			f.Decls = decls
			ast.FileExports(f)
			self.addToPackage(key, f.Decls)
		}
	}
	return defpkgname
}

func (self *AutoCompleteContext) beautifyIdent(ident string) string {
	foreign, ok := self.foreigns[ident]
	if ok {
		return foreign.Abbrev
	}
	return ident
}

func getArrayLen(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.BasicLit:
		return string(t.Value)
	case *ast.Ellipsis:
		return "..."
	}
	return ""
}

func (self *PackageFile) foreignifyFuncFieldList(f *ast.FieldList) {
	if f == nil {
		return
	}

	for _, field := range f.List {
		self.foreignifyTypeExpr(field.Type)
	}
}

// I know that probably such word doesn't even exists, but 'foreignify' is a
// perfect description for what it does. It unties type expression from its
// file scope.
func (self *PackageFile) foreignifyTypeExpr(e ast.Expr) {
	switch t := e.(type) {
	case *ast.StarExpr:
		self.foreignifyTypeExpr(t.X)
	case *ast.Ident:
		realname := self.moduleRealName(t.Name())
		if realname != "" {
			t.Obj.Name = self.ctx.genForeignPackageAlias(t.Name(), realname)
		}
	case *ast.ArrayType:
		self.foreignifyTypeExpr(t.Elt)
	case *ast.SelectorExpr:
		self.foreignifyTypeExpr(t.X)
	case *ast.FuncType:
		self.foreignifyFuncFieldList(t.Params)
		self.foreignifyFuncFieldList(t.Results)
	case *ast.MapType:
		self.foreignifyTypeExpr(t.Key)
		self.foreignifyTypeExpr(t.Value)
	case *ast.ChanType:
		self.foreignifyTypeExpr(t.Value)
	}
}

func (self *AutoCompleteContext) prettyPrintTypeExpr(out io.Writer, e ast.Expr) {
	switch t := e.(type) {
	case *ast.StarExpr:
		fmt.Fprintf(out, "*")
		self.prettyPrintTypeExpr(out, t.X)
	case *ast.Ident:
		fmt.Fprintf(out, self.beautifyIdent(t.Name()))
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
		self.prettyPrintTypeExpr(out, t.Elt)
	case *ast.SelectorExpr:
		self.prettyPrintTypeExpr(out, t.X)
		fmt.Fprintf(out, ".%s", t.Sel.Name())
	case *ast.FuncType:
		fmt.Fprintf(out, "func(")
		self.prettyPrintFuncFieldList(out, t.Params)
		fmt.Fprintf(out, ")")

		buf := bytes.NewBuffer(make([]byte, 0, 256))
		nresults := self.prettyPrintFuncFieldList(buf, t.Results)
		if nresults > 0 {
			results := buf.String()
			if strings.Index(results, ",") != -1 {
				results = "(" + results + ")"
			}
			fmt.Fprintf(out, " %s", results)
		}
	case *ast.MapType:
		fmt.Fprintf(out, "map[")
		self.prettyPrintTypeExpr(out, t.Key)
		fmt.Fprintf(out, "]")
		self.prettyPrintTypeExpr(out, t.Value)
	case *ast.InterfaceType:
		fmt.Fprintf(out, "interface{}")
	case *ast.Ellipsis:
		fmt.Fprintf(out, "...")
		self.prettyPrintTypeExpr(out, t.Elt)
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
		self.prettyPrintTypeExpr(out, t.Value)
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

func (self *AutoCompleteContext) prettyPrintFuncFieldList(out io.Writer, f *ast.FieldList) int {
	count := 0
	if f == nil {
		return count
	}
	for i, field := range f.List {
		// names
		if field.Names != nil {
			for j, name := range field.Names {
				fmt.Fprintf(out, "%s", name.Name())
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
		self.prettyPrintTypeExpr(out, field.Type)

		// ,
		if i != len(f.List)-1 {
			fmt.Fprintf(out, ", ")
		}
	}
	return count
}

func startsWith(s, prefix string) bool {
	if len(s) >= len(prefix) && s[0:len(prefix)] == prefix {
		return true
	}
	return false
}

func findGlobalFile(imp string) string {
	goroot := os.Getenv("GOROOT")
	goarch := os.Getenv("GOARCH")
	goos := os.Getenv("GOOS")

	return fmt.Sprintf("%s/pkg/%s_%s/%s.a", goroot, goos, goarch, imp)
}

func (self *PackageFile) findFile(imp string) string {
	if imp[0] == '.' {
		dir, _ := path.Split(self.name)
		return fmt.Sprintf("%s.a", path.Join(dir, imp))
	}
	return findGlobalFile(imp)
}

func pathAndAlias(imp *ast.ImportSpec) (string, string) {
	path := string(imp.Path.Value)
	alias := ""
	if imp.Name != nil {
		alias = imp.Name.Name()
	}
	path = path[1:len(path)-1]
	return path, alias
}

func (self *PackageFile) processImportSpec(imp *ast.ImportSpec) {
	path, alias := pathAndAlias(imp)
	self.processPackage(self.findFile(path), path, alias)
}

func (self *AutoCompleteContext) cursorIn(block *ast.BlockStmt) bool {
	if self.cursor == -1 || block == nil {
		return false
	}

	if self.cursor >= block.Offset && self.cursor <= block.Rbrace.Offset {
		return true
	}
	return false
}

func (self *PackageFile) processFieldList(fieldList *ast.FieldList) {
	if fieldList != nil {
		decls := astFieldListToDecls(fieldList, DECL_VAR, self)
		for _, d := range decls {
			self.l[d.Name] = d
		}
	}
}

func (self *PackageFile) addVarDecl(d *Decl) {
	decl, ok := self.l[d.Name]
	if ok {
		decl.Expand(d)
	} else {
		self.l[d.Name] = d
	}
}

func (self *PackageFile) processAssignStmt(a *ast.AssignStmt) {
	if a.Tok != token.DEFINE || a.TokPos.Offset > self.ctx.cursor {
		return
	}

	names := make([]string, len(a.Lhs))
	for i, name := range a.Lhs {
		id, ok := name.(*ast.Ident)
		if !ok {
			// something is wrong, just ignore the whole stmt
			return
		}
		names[i] = id.Name()
	}

	for i, name := range names {
		var value ast.Expr
		valueindex := -1
		if len(a.Rhs) > 1 {
			value = a.Rhs[i]
		} else {
			value = a.Rhs[0]
			valueindex = i
		}

		d := NewDeclVar(name, nil, value, valueindex, self)
		if d == nil {
			continue
		}

		self.addVarDecl(d)
	}
}

func (self *PackageFile) processRangeStmt(a *ast.RangeStmt) {
	if !self.ctx.cursorIn(a.Body) {
		return
	}
	if a.Tok == token.DEFINE {
		var t1, t2 ast.Expr
		t1 = NewDeclVar("tmp", nil, a.X, -1, self).InferType()
		if t1 != nil {
			// figure out range Key, Value types
			switch t := t1.(type) {
			case *ast.Ident:
				// string
				if t.Name() == "string" {
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

			if t, ok := a.Key.(*ast.Ident); ok {
				d := NewDeclVar(t.Name(), t1, nil, -1, self)
				if d != nil {
					self.addVarDecl(d)
				}
			}

			if a.Value != nil {
				if t, ok := a.Value.(*ast.Ident); ok {
					d := NewDeclVar(t.Name(), t2, nil, -1, self)
					if d != nil {
						self.addVarDecl(d)
					}
				}
			}
		}
	}

	self.processBlockStmt(a.Body)
}

func (self *PackageFile) processSwitchStmt(a *ast.SwitchStmt) {
	if !self.ctx.cursorIn(a.Body) {
		return
	}
	self.processStmt(a.Init)
	var lastCursorAfter *ast.CaseClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.CaseClause); self.ctx.cursor > cc.Colon.Offset {
			lastCursorAfter = cc
		}
	}
	if lastCursorAfter != nil {
		for _, s := range lastCursorAfter.Body {
			self.processStmt(s)
		}
	}
}

func (self *PackageFile) processTypeSwitchStmt(a *ast.TypeSwitchStmt) {
	if !self.ctx.cursorIn(a.Body) {
		return
	}
	self.processStmt(a.Init)
	// type var
	var tv *Decl
	lhs := a.Assign.(*ast.AssignStmt).Lhs
	rhs := a.Assign.(*ast.AssignStmt).Rhs
	if lhs != nil && len(lhs) == 1 {
		tvname := lhs[0].(*ast.Ident).Name()
		tv = NewDeclVar(tvname, nil, rhs[0], -1, self)
	}

	var lastCursorAfter *ast.TypeCaseClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.TypeCaseClause); self.ctx.cursor > cc.Colon.Offset {
			lastCursorAfter = cc
		}
	}

	if lastCursorAfter != nil {
		if tv != nil {
			if lastCursorAfter.Types != nil && len(lastCursorAfter.Types) == 1 {
				tv.Type = lastCursorAfter.Types[0]
				tv.Value = nil
			}
			self.addVarDecl(tv)
		}
		for _, s := range lastCursorAfter.Body {
			self.processStmt(s)
		}
	}
}

func (self *PackageFile) processSelectStmt(a *ast.SelectStmt) {
	if !self.ctx.cursorIn(a.Body) {
		return
	}
	var lastCursorAfter *ast.CommClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.CommClause); self.ctx.cursor > cc.Colon.Offset {
			lastCursorAfter = cc
		}
	}

	if lastCursorAfter != nil {
		if lastCursorAfter.Lhs != nil && lastCursorAfter.Tok == token.DEFINE {
			vname := lastCursorAfter.Lhs.(*ast.Ident).Name()
			v := NewDeclVar(vname, nil, lastCursorAfter.Rhs, -1, self)
			self.addVarDecl(v)
		}
		for _, s := range lastCursorAfter.Body {
			self.processStmt(s)
		}
	}
}

func (self *PackageFile) processStmt(stmt ast.Stmt) {
	switch t := stmt.(type) {
	case *ast.DeclStmt:
		self.processDecl(t.Decl, true)
	case *ast.AssignStmt:
		self.processAssignStmt(t)
	case *ast.IfStmt:
		if self.ctx.cursorIn(t.Body) {
			self.processStmt(t.Init)
			self.processBlockStmt(t.Body)
		}
		self.processStmt(t.Else)
	case *ast.BlockStmt:
		self.processBlockStmt(t)
	case *ast.RangeStmt:
		self.processRangeStmt(t)
	case *ast.ForStmt:
		if self.ctx.cursorIn(t.Body) {
			self.processStmt(t.Init)
			self.processBlockStmt(t.Body)
		}
	case *ast.SwitchStmt:
		self.processSwitchStmt(t)
	case *ast.TypeSwitchStmt:
		self.processTypeSwitchStmt(t)
	case *ast.SelectStmt:
		self.processSelectStmt(t)
	case *ast.LabeledStmt:
		self.processStmt(t.Stmt)
	}
}

func (self *PackageFile) processBlockStmt(block *ast.BlockStmt) {
	if block != nil && self.ctx.cursorIn(block) {
		for _, stmt := range block.List {
			self.processStmt(stmt)
		}

		// hack to process all func literals
		v := new(FuncLitVisitor)
		v.ctx = self
		ast.Walk(v, block)
	}
}

type FuncLitVisitor struct {
	ctx *PackageFile
}

func (v *FuncLitVisitor) Visit(node interface{}) ast.Visitor {
	if t, ok := node.(*ast.FuncLit); ok {
		v.ctx.processFieldList(t.Type.Params)
		v.ctx.processFieldList(t.Type.Results)
		v.ctx.processBlockStmt(t.Body)
		return nil
	}
	return v
}

func (self *PackageFile) processDecl(decl ast.Decl, parseLocals bool) {
	switch t := decl.(type) {
	case *ast.GenDecl:
		if parseLocals {
			// break if we're too far
			if t.Offset > self.ctx.cursor {
				return
			}
		} else {
			switch t.Tok {
			case token.IMPORT:
				for _, spec := range t.Specs {
					imp, ok := spec.(*ast.ImportSpec)
					if !ok {
						panic("Fail")
					}
					self.processImportSpec(imp)
				}
			}
		}
	case *ast.FuncDecl:
		if parseLocals && self.ctx.cursorIn(t.Body) {
			// put into 'locals' (if any):
			// 1. method var
			// 2. args vars
			// 3. results vars
			self.processFieldList(t.Recv)
			self.processFieldList(t.Type.Params)
			self.processFieldList(t.Type.Results)
			self.processBlockStmt(t.Body)
		}
	}

	decls := splitDecls(decl)
	for _, decl := range decls {
		names := declNames(decl)
		values := declValues(decl)

		for i, name := range names {
			var value ast.Expr = nil
			valueindex := -1
			if values != nil {
				if len(values) > 1 {
					value = values[i]
				} else {
					value = values[0]
					valueindex = i
				}
			}

			d := NewDeclFromAstDecl(name, decl, value, valueindex, self)
			if d == nil {
				continue
			}

			methodof := MethodOf(decl)
			if methodof != "" {
				decl, ok := self.l[methodof]
				if ok {
					decl.AddChild(d)
				} else {
					decl = NewDecl(methodof, DECL_TYPE, self)
					self.l[methodof] = decl
					decl.AddChild(d)
				}
			} else {
				self.addVarDecl(d)
			}
		}
	}
}

func packageName(file *ast.File) string {
	if file.Name != nil {
		return file.Name.Name()
	}
	return ""
}

func (self *PackageFile) resetLocals() {
	self.l = make(map[string]*Decl)
	self.cfns = make(map[string]string)
	self.cfnsReverse = make(map[string]string)
}

func (self *PackageFile) processData(data []byte) string {
	// drop namespace and locals
	self.resetLocals()

	tc := new(TokCollection)
	cur, file, block := tc.ripOffDecl(data, self.ctx.cursor)
	if block != nil {
		// process file without locals first
		file, _ := parser.ParseFile("", file, nil, 0)
		for _, decl := range file.Decls {
			self.processDecl(decl, false)
		}

		// parse local function
		self.ctx.cursor = cur
		decls, _ := parser.ParseDeclList("", block, nil)
		for _, decl := range decls {
			self.processDecl(decl, true)
		}
		return packageName(file)
	} else {
		// probably we don't have locals anyway
		file, _ := parser.ParseFile("", file, nil, 0)
		for _, decl := range file.Decls {
			self.processDecl(decl, false)
		}
		return packageName(file)
	}
	return ""
}

func (self *PackageFile) processFile(filename string) {
	self.resetLocals()

	file, _ := parser.ParseFile(filename, nil, nil, 0)
	for _, decl := range file.Decls {
		self.processDecl(decl, false)
	}
}

func (self *PackageFile) updateCache() {
	stat, err := os.Stat(self.name)
	if err != nil {
		panic(err.String())
	}

	if self.mtime != stat.Mtime_ns {
		self.processFile(self.name)
		self.mtime = stat.Mtime_ns
	}
}

// represents foreign package (e.g. a package in the package, not imported directly)
type ForeignPackage struct {
	Abbrev string // local nice name, like "ast"
	Unique string // real global unique name, like "go/ast"
}

type ModuleCache struct {
	// full name (example: "go/ast", "/full/path/to/local/localpackage.a")
	// NOTE: it's not a file name, it is a name in 'm' map
	name string
	filename string
	mtime int64 // modification time
	checked bool // is checked by current iteration?
	defalias string // default alias (example: "ast", "localpackage")
	ctx *AutoCompleteContext
}

func NewModuleCache(filename, uniquename string, ctx *AutoCompleteContext) *ModuleCache {
	m := new(ModuleCache)
	m.name = uniquename
	m.filename = filename
	m.mtime = 0
	m.checked = false
	m.defalias = ""
	m.ctx = ctx
	m.updateCache()
	return m
}

// create a package that always resides in cache, useful for built-in packages
func NewModuleCacheForever(defalias string) *ModuleCache {
	m := new(ModuleCache)
	m.mtime = -1
	m.defalias = defalias
	return m
}

func (self *ModuleCache) updateCache() {
	if self.checked || self.mtime == -1 {
		return
	}
	self.checked = true
	stat, err := os.Stat(self.filename)
	if err != nil {
		panic(err.String())
	}

	if self.mtime != stat.Mtime_ns {
		// delete old module
		self.ctx.m[self.name] = nil, false
		self.mtime = stat.Mtime_ns

		// try load new
		data, err := ioutil.ReadFile(self.filename)
		if err != nil {
			return
		}
		self.defalias = self.ctx.processPackageData(self.name, string(data))
	}
}

type PackageFile struct {
	name string
	packageName string

	// current file namespace (used for imported modules)
	// imported name -> full name (as key in m)
	cfns map[string]string
	cfnsReverse map[string]string
	l map[string]*Decl
	mtime int64
	ctx *AutoCompleteContext

	destroy bool // used only in cache ops
}

func filePackageName(filename string) string {
	file, _ := parser.ParseFile(filename, nil, nil, parser.PackageClauseOnly)
	return file.Name.Name()
}

func NewPackageFileFromFile(ctx *AutoCompleteContext, name, packageName string) *PackageFile {
	p := new(PackageFile)
	p.name = name
	p.packageName = packageName
	p.cfns = make(map[string]string)
	p.cfnsReverse = make(map[string]string)
	p.l = make(map[string]*Decl)
	p.mtime = 0
	p.ctx = ctx
	p.updateCache()
	return p
}

func NewPackageFile(ctx *AutoCompleteContext) *PackageFile {
	p := new(PackageFile)
	p.name = ""
	p.packageName = ""
	p.cfns = make(map[string]string)
	p.cfnsReverse = make(map[string]string)
	p.l = make(map[string]*Decl)
	p.mtime = 0
	p.ctx = ctx
	return p
}

type AutoCompleteContext struct {
	m map[string]*Decl // all modules (lifetime cache)
	foreigns map[string]ForeignPackage

	current *PackageFile
	others map[string]*PackageFile

	cache map[string]*ModuleCache

	debuglog io.Writer

	// cursor position, in bytes, -1 if unknown
	// used for parsing function locals (only in the current function)
	cursor int
}

func NewAutoCompleteContext() *AutoCompleteContext {
	self := new(AutoCompleteContext)
	self.m = make(map[string]*Decl)
	self.foreigns = make(map[string]ForeignPackage)
	self.current = NewPackageFile(self)
	self.others = make(map[string]*PackageFile)
	self.cache = make(map[string]*ModuleCache)
	self.cursor = -1
	self.addBuiltinUnsafe()
	return self
}

func (self *AutoCompleteContext) debugLog(f string, a ...interface{}) {
	if self.debuglog != nil {
		fmt.Fprintf(self.debuglog, f, a)
	}
}

func (self *AutoCompleteContext) dropModulesCheckedStatus() {
	for _, m := range self.cache {
		m.checked = false
	}
}

func (self *AutoCompleteContext) addToPackage(globalname string, decls []ast.Decl) {
	if self.m[globalname] == nil {
		// I use self.current here as a file, because global package
		// cache doesn't tied to a file scope. All its idents are safe
		// to use in any file scope (they are foreignified)
		self.m[globalname] = NewDecl(globalname, DECL_MODULE, self.current)
	}
	module := self.m[globalname]

	for _, decl := range decls {
		decls := splitDecls(decl)
		for _, decl := range decls {
			names := declNames(decl)
			values := declValues(decl)

			for i, name := range names {
				var value ast.Expr = nil
				valueindex := -1
				if values != nil {
					if len(values) > 1 {
						value = values[i]
					} else {
						value = values[0]
						valueindex = i
					}
				}

				d := NewDeclFromAstDecl(name, decl, value, valueindex, self.current)
				if d == nil {
					continue
				}

				methodof := MethodOf(decl)
				if methodof != "" {
					if !ast.IsExported(methodof) {
						continue
					}
					decl := module.FindChild(methodof)
					if decl != nil {
						decl.AddChild(d)
					} else {
						decl = NewDecl(methodof, DECL_TYPE, self.current)
						module.AddChild(decl)
						decl.AddChild(d)
					}
				} else {
					decl := module.FindChild(d.Name)
					if decl != nil {
						decl.Expand(d)
					} else {
						module.AddChild(d)
					}
				}
			}
		}
	}
}

func (self *PackageFile) addPackageAlias(alias string, globalname string) {
	self.cfns[alias] = globalname
	self.cfnsReverse[globalname] = alias
}

func (self *AutoCompleteContext) genForeignPackageAlias(alias, globalname string) string {
	sum := crc32.ChecksumIEEE([]byte(alias + globalname))
	name := fmt.Sprintf("__%X__", sum)
	self.foreigns[name] = ForeignPackage{alias, globalname}
	return name
}

func (self *PackageFile) findDeclByPath(path string) *Decl {
	s := strings.Split(path, ".", -1)
	switch len(s) {
	case 1:
		return self.findDecl(s[0])
	case 2:
		d := self.findDecl(s[0])
		if d != nil {
			return d.FindChild(s[1])
		}
	}
	return nil
}

func (self *PackageFile) moduleRealName(name string) string {
	realname, ok := self.cfns[name]
	if ok {
		_, ok := self.ctx.m[realname]
		if ok {
			return realname
		}
	}
	return ""
}

func (self *PackageFile) localModuleName(name string) string {
	name, ok := self.cfnsReverse[name]
	if ok {
		return name
	}
	return ""
}

func (self *PackageFile) findDecl(name string) *Decl {
	// first, check cfns
	realname, ok := self.cfns[name]
	if ok {
		d, ok := self.ctx.m[realname]
		if ok {
			return d
		}
	}

	// check and merge locals in all package files
	decl, ok := self.ctx.current.l[name]
	if ok {
		decl = decl.DeepCopy()
	}

	for _, f := range self.ctx.others {
		d, ok := f.l[name]
		if ok {
			if decl == nil {
				decl = d.DeepCopy()
			} else {
				decl.Expand(d)
			}
		}
	}
	if decl != nil {
		return decl
	}

	// then check foreigns
	foreign, ok := self.ctx.foreigns[name]
	if ok {
		d, ok := self.ctx.m[foreign.Unique]
		if ok {
			return d
		}
	}
	return nil
}

//-------------------------------------------------------------------------
// Sort interface for TriStringArrays
//-------------------------------------------------------------------------

type TriStringArrays struct {
	first []string
	second []string
	third []string
}

func (self TriStringArrays) Len() int {
	return len(self.first)
}

func (self TriStringArrays) Less(i, j int) bool {
	return self.third[i] + self.first[i] < self.third[j] + self.first[j]
}

func (self TriStringArrays) Swap(i, j int) {
	self.first[i], self.first[j] = self.first[j], self.first[i]
	self.second[i], self.second[j] = self.second[j], self.second[i]
	self.third[i], self.third[j] = self.third[j], self.third[i]
}

//-------------------------------------------------------------------------

func (self *AutoCompleteContext) processOtherPackageFiles() {
	packageName := self.current.packageName
	filename := self.current.name

	dir, file := path.Split(filename)
	filesInDir, err := ioutil.ReadDir(dir)
	if err != nil {
		panic(err.String())
	}

	newothers := make(map[string]*PackageFile)
	for _, stat := range filesInDir {
		ok, _ := path.Match("*.go", stat.Name)
		if ok && stat.Name != file {
			filepath := path.Join(dir, stat.Name)
			oldother, ok := self.others[filepath]
			if ok && oldother.packageName == packageName {
				newothers[filepath] = oldother
				oldother.updateCache()
			} else {
				pkg := filePackageName(filepath)
				if pkg == packageName {
					newothers[filepath] = NewPackageFileFromFile(self, filepath, packageName)
				}
			}
		}
	}
	self.others = newothers
}

type OutBuffers struct {
	names, types, classes *bytes.Buffer
}

func NewOutBuffers() *OutBuffers {
	b := new(OutBuffers)
	b.names = bytes.NewBuffer(make([]byte, 0, 4096))
	b.types = bytes.NewBuffer(make([]byte, 0, 4096))
	b.classes = bytes.NewBuffer(make([]byte, 0, 4096))
	return b
}

func matchClass(declclass int, class int) bool {
	if class == declclass {
		return true
	}
	return false
}

func (self *OutBuffers) appendPackage(p, pak string, class int) {
	if startsWith(pak, p) || matchClass(DECL_MODULE, class) {
		fmt.Fprintf(self.names, "%s\n", pak)
		fmt.Fprintf(self.types, "\n")
		fmt.Fprintf(self.classes, "module\n")
	}
}

func (self *OutBuffers) appendDecl(p string, decl *Decl, class int) {
	if decl.Matches(p) || matchClass(decl.Class, class) {
		fmt.Fprintf(self.names, "%s\n", decl.Name)
		decl.PrettyPrintType(self.types, decl.File.ctx)
		fmt.Fprintf(self.types, "\n")
		fmt.Fprintf(self.classes, "%s\n", decl.ClassName())
	}
}

func (self *OutBuffers) appendEmbedded(p string, decl *Decl, class int) {
	if decl.Embedded != nil {
		for _, emb := range decl.Embedded {
			typedecl := typeToDecl(emb, decl.File)
			if typedecl != nil {
				for _, c := range typedecl.Children {
					self.appendDecl(p, c, class)
				}
				self.appendEmbedded(p, typedecl, class)
			}
		}
	}
}

// return three slices of the same length containing:
// 1. apropos names
// 2. apropos types (pretty-printed)
// 3. apropos classes
func (self *AutoCompleteContext) Apropos(file []byte, filename string, cursor int) ([]string, []string, []string, int) {
	self.dropModulesCheckedStatus()
	self.cursor = cursor
	self.current.name = filename
	self.current.packageName = self.current.processData(file)
	if filename != "" {
		self.processOtherPackageFiles()
	}

	b := NewOutBuffers()

	partial := 0
	da := self.deduceDecl(file, cursor)
	if da != nil {
		class := -1
		switch da.Partial {
		case "const":
			class = DECL_CONST
		case "var":
			class = DECL_VAR
		case "type":
			class = DECL_TYPE
		case "func":
			class = DECL_FUNC
		case "module":
			class = DECL_MODULE
		}
		if da.Decl == nil {
			// In case if no declaraion is a subject of completion, propose all:

			// modules
			for key, value := range self.current.cfns {
				if _, ok := self.m[value]; ok {
					b.appendPackage(da.Partial, key, class)
				}
			}
			// and locals
			for _, value := range self.current.l {
				value.InferType()
				b.appendDecl(da.Partial, value, class)
			}
			for _, other := range self.others {
				for _, value := range other.l {
					value.InferType()
					b.appendDecl(da.Partial, value, class)
				}
			}
		} else {
			// propose all children of a subject declaration and
			// propose all children of its embedded types
			for _, decl := range da.Decl.Children {
				b.appendDecl(da.Partial, decl, class)
			}
			b.appendEmbedded(da.Partial, da.Decl, class)
		}
		partial = len(da.Partial)
	}

	if b.names.Len() == 0 || b.types.Len() == 0 || b.classes.Len() == 0 {
		return nil, nil, nil, 0
	}

	var tri TriStringArrays
	tri.first = strings.Split(b.names.String()[0:b.names.Len()-1], "\n", -1)
	tri.second = strings.Split(b.types.String()[0:b.types.Len()-1], "\n", -1)
	tri.third = strings.Split(b.classes.String()[0:b.classes.Len()-1], "\n", -1)
	sort.Sort(tri)
	return tri.first, tri.second, tri.third, partial
}

func (self *AutoCompleteContext) Status() string {
	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	fmt.Fprintf(buf, "Number of top level packages: %d\n", len(self.m))
	if len(self.m) > 0 {
		fmt.Fprintf(buf, "\nListing packages:\n")
		for key, decl := range self.m {
			fmt.Fprintf(buf, "'%s' : %s\n", key, decl.Name)
		}
		fmt.Fprintf(buf, "\n")
	}
	if len(self.foreigns) > 0 {
		fmt.Fprintf(buf, "\nListing foreigns:\n")
		for key, foreign := range self.foreigns {
			fmt.Fprintf(buf, "%s:\n", key)
			fmt.Fprintf(buf, "\t%s\n\t%s\n", foreign.Abbrev, foreign.Unique)
		}
	}
	return buf.String()
}
