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

	// test {

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

func (self *TokCollection) findExpBeg(pos int) int {
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

func (self *TokCollection) findExpEnd(pos int) int {
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

	return self.findExpBeg(pos), self.findExpEnd(pos)
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
			pkg = self.addForeignAlias(identifyPackage(s[b+1:e]), s[b+1:e])
			i++ // skip to a first symbol after second '"'
			s = s[0:b] + pkg + s[i:] // strip package clause completely
			i = b
		} else {
			pkg = self.addForeignAlias(identifyPackage(curpkg), curpkg)
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

func (self *AutoCompleteContext) processPackage(filename, uniquename, pkgname string) {
	// TODO: deal with packages imported in the current namespace
	if self.cache[filename] {
		self.addAlias(self.m[uniquename].Name, uniquename)
		return
	}
	self.cache[filename] = true

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return
	}
	s := string(data)

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

	if pkgname == "" {
		pkgname = s[len("package "):i-1]
	}
	self.addAlias(pkgname, uniquename)

	if self.debuglog != nil {
		fmt.Fprintf(self.debuglog, "parsing package '%s'...\n", pkgname)
	}
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
			if self.debuglog != nil {
				fmt.Fprintf(self.debuglog, "\t%s: OK (ndecls: %d)\n", key, len(decls))
			}
			f := new(ast.File) // fake file
			f.Decls = decls
			ast.FileExports(f)
			localname := ""
			if key == uniquename {
				localname = pkgname
			}
			self.add(key, localname, f.Decls)
		}
	}
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
	case *ast.CallExpr:
		self.prettyPrintTypeExpr(out, t.Fun)
	case *ast.ChanType:
		// TODO: different types of channels
		fmt.Fprintf(out, "chan ")
		self.prettyPrintTypeExpr(out, t.Value)
	default:
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

func findFile(imp string) string {
	goroot := os.Getenv("GOROOT")
	goarch := os.Getenv("GOARCH")
	goos := os.Getenv("GOOS")

	return fmt.Sprintf("%s/pkg/%s_%s/%s.a", goroot, goos, goarch, imp)
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

func (self *AutoCompleteContext) processImportSpec(imp *ast.ImportSpec) {
	path, alias := pathAndAlias(imp)
	self.processPackage(findFile(path), path, alias)
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

func (self *AutoCompleteContext) processFieldList(fieldList *ast.FieldList) {
	if fieldList != nil {
		decls := astFieldListToDecls(fieldList, DECL_VAR)
		for _, d := range decls {
			self.l[d.Name] = d
		}
	}
}

func (self *AutoCompleteContext) addVarDecl(d *Decl) {
	decl, ok := self.l[d.Name]
	if ok {
		decl.Expand(d)
	} else {
		self.l[d.Name] = d
	}
}

func (self *AutoCompleteContext) processAssignStmt(a *ast.AssignStmt) {
	if a.Tok != token.DEFINE || a.TokPos.Offset > self.cursor {
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

		d := NewDeclVar(name, nil, value, valueindex)
		if d == nil {
			continue
		}

		self.addVarDecl(d)
	}
}

func (self *AutoCompleteContext) processRangeStmt(a *ast.RangeStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	if a.Tok == token.DEFINE {
		var t1, t2 ast.Expr
		t1 = NewDeclVar("tmp", nil, a.X, -1).InferType(self)
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
			// TODO: add channels
			default:
				t1, t2 = nil, nil
			}

			if t, ok := a.Key.(*ast.Ident); ok {
				d := NewDeclVar(t.Name(), t1, nil, -1)
				if d != nil {
					self.addVarDecl(d)
				}
			}

			if a.Value != nil {
				if t, ok := a.Value.(*ast.Ident); ok {
					d := NewDeclVar(t.Name(), t2, nil, -1)
					if d != nil {
						self.addVarDecl(d)
					}
				}
			}
		}
	}

	self.processBlockStmt(a.Body)
}

func (self *AutoCompleteContext) processSwitchStmt(a *ast.SwitchStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	self.processStmt(a.Init)
	var lastCursorAfter *ast.CaseClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.CaseClause); self.cursor > cc.Colon.Offset {
			lastCursorAfter = cc
		}
	}
	if lastCursorAfter != nil {
		for _, s := range lastCursorAfter.Body {
			self.processStmt(s)
		}
	}
}

func (self *AutoCompleteContext) processTypeSwitchStmt(a *ast.TypeSwitchStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	self.processStmt(a.Init)
	// type var
	var tv *Decl
	lhs := a.Assign.(*ast.AssignStmt).Lhs
	rhs := a.Assign.(*ast.AssignStmt).Rhs
	if lhs != nil && len(lhs) == 1 {
		tvname := lhs[0].(*ast.Ident).Name()
		tv = NewDeclVar(tvname, nil, rhs[0], -1)
	}

	var lastCursorAfter *ast.TypeCaseClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.TypeCaseClause); self.cursor > cc.Colon.Offset {
			lastCursorAfter = cc
		}
	}

	if lastCursorAfter != nil {
		if tv != nil {
			if lastCursorAfter.Types != nil && len(lastCursorAfter.Types) == 1 {
				tv.Type = lastCursorAfter.Types[0]
			}
			self.addVarDecl(tv)
		}
		for _, s := range lastCursorAfter.Body {
			self.processStmt(s)
		}
	}
}

func (self *AutoCompleteContext) processStmt(stmt ast.Stmt) {
	// TODO: we need to process func literals somehow too as locals
	switch t := stmt.(type) {
	case *ast.DeclStmt:
		self.processDecl(t.Decl, true)
	case *ast.AssignStmt:
		self.processAssignStmt(t)
	case *ast.IfStmt:
		if self.cursorIn(t.Body) {
			self.processStmt(t.Init)
			self.processBlockStmt(t.Body)
		}
		self.processStmt(t.Else)
	case *ast.BlockStmt:
		self.processBlockStmt(t)
	case *ast.RangeStmt:
		self.processRangeStmt(t)
	case *ast.ForStmt:
		if self.cursorIn(t.Body) {
			self.processStmt(t.Init)
			self.processBlockStmt(t.Body)
		}
	case *ast.SwitchStmt:
		self.processSwitchStmt(t)
	case *ast.TypeSwitchStmt:
		self.processTypeSwitchStmt(t)
	// TODO: *ast.SelectStmt
	}
}

func (self *AutoCompleteContext) processBlockStmt(block *ast.BlockStmt) {
	if block != nil && self.cursorIn(block) {
		for _, stmt := range block.List {
			self.processStmt(stmt)
		}
	}
}

func (self *AutoCompleteContext) processDecl(decl ast.Decl, parseLocals bool) {
	switch t := decl.(type) {
	case *ast.GenDecl:
		if parseLocals {
			// break if we're too far
			if t.Offset > self.cursor {
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
		if parseLocals && self.cursorIn(t.Body) {
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

			d := astDeclToDecl(name, decl, value, valueindex)
			if d == nil {
				continue
			}

			methodof := MethodOf(decl)
			if methodof != "" {
				decl, ok := self.l[methodof]
				if ok {
					decl.AddChild(d)
				} else {
					decl = NewDecl(methodof, DECL_TYPE)
					self.l[methodof] = decl
					decl.AddChild(d)
				}
			} else {
				decl, ok := self.l[d.Name]
				if ok {
					decl.Expand(d)
				} else {
					self.l[d.Name] = d
				}
			}
		}
	}
}

func (self *AutoCompleteContext) processData(data []byte) {
	// drop namespace and locals
	self.l = make(map[string]*Decl)
	self.cfns = make(map[string]string)

	tc := new(TokCollection)
	cur, file, block := tc.ripOffDecl(data, self.cursor)
	if block != nil {
		// process file without locals first
		file, _ := parser.ParseFile("", file, nil, 0)
		for _, decl := range file.Decls {
			self.processDecl(decl, false)
		}

		// parse local function
		self.cursor = cur
		decls, _ := parser.ParseDeclList("", block, nil)
		for _, decl := range decls {
			self.processDecl(decl, true)
		}
	} else {
		// probably we don't have locals anyway
		file, _ := parser.ParseFile("", file, nil, 0)
		for _, decl := range file.Decls {
			self.processDecl(decl, false)
		}
	}
}

// represents foreign package (e.g. a package in the package, not imported directly)
type ForeignPackage struct {
	Abbrev string // nice name, like "ast"
	Unique string // real global unique name, like "go/ast"
}

type AutoCompleteContext struct {
	m map[string]*Decl // all modules (lifetime cache)
	l map[string]*Decl // locals

	// current file namespace (used for modules):
	// alias name ->
	//	unique package name
	cfns map[string]string
	foreigns map[string]ForeignPackage

	cache map[string]bool // stupid, temporary

	debuglog io.Writer

	// cursor position, in bytes, -1 if unknown
	// used for parsing function locals (only in the current function)
	cursor int
}

func NewAutoCompleteContext() *AutoCompleteContext {
	self := new(AutoCompleteContext)
	self.m = make(map[string]*Decl)
	self.l = make(map[string]*Decl)
	self.cfns = make(map[string]string)
	self.foreigns = make(map[string]ForeignPackage)
	self.cache = make(map[string]bool)
	self.cursor = -1
	return self
}

func (self *AutoCompleteContext) add(globalname, localname string, decls []ast.Decl) {
	if self.m[globalname] == nil {
		self.m[globalname] = NewDecl(localname, DECL_MODULE)
	}

	if self.m[globalname].Name == "" && localname != "" {
		self.m[globalname].Name = localname
	}

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

				d := astDeclToDecl(name, decl, value, valueindex)
				if d == nil {
					continue
				}

				methodof := MethodOf(decl)
				if methodof != "" {
					if !ast.IsExported(methodof) {
						continue
					}
					decl := self.m[globalname].FindChild(methodof)
					if decl != nil {
						decl.AddChild(d)
					} else {
						decl = NewDecl(methodof, DECL_TYPE)
						self.m[globalname].AddChild(decl)
						decl.AddChild(d)
					}
				} else {
					decl := self.m[globalname].FindChild(d.Name)
					if decl != nil {
						decl.Expand(d)
					} else {
						self.m[globalname].AddChild(d)
					}
				}
			}
		}
	}
}

func (self *AutoCompleteContext) addAlias(alias string, globalname string) {
	self.cfns[alias] = globalname
}

func (self *AutoCompleteContext) addForeignAlias(alias string, globalname string) string {
	sum := crc32.ChecksumIEEE([]byte(globalname))
	name := fmt.Sprintf("__%X__", sum)
	self.foreigns[name] = ForeignPackage{alias, globalname}
	return name
}

func (self *AutoCompleteContext) findDeclByPath(path string) *Decl {
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

func (self *AutoCompleteContext) findDecl(name string) *Decl {
	// first, check cfns and locals
	realname, ok := self.cfns[name]
	if ok {
		d, ok := self.m[realname]
		if ok {
			return d
		}
	}

	d, ok := self.l[name]
	if ok {
		return d
	}

	// then check foreigns
	foreign, ok := self.foreigns[name]
	if ok {
		d, ok := self.m[foreign.Unique]
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

func (self *AutoCompleteContext) appendDecl(names, types, classes *bytes.Buffer, p string, decl *Decl) {
	if decl.Matches(p) {
		fmt.Fprintf(names, "%s\n", decl.Name)
		decl.PrettyPrintType(types, self)
		fmt.Fprintf(types, "\n")
		fmt.Fprintf(classes, "%s\n", decl.ClassName())
	}
}

// return three slices of the same length containing:
// 1. apropos names
// 2. apropos types (pretty-printed)
// 3. apropos classes
func (self *AutoCompleteContext) Apropos(file []byte, apropos string, cursor int) ([]string, []string, []string) {
	self.cursor = cursor
	self.processData(file)

	out_names := bytes.NewBuffer(make([]byte, 0, 4096))
	out_types := bytes.NewBuffer(make([]byte, 0, 4096))
	out_classes := bytes.NewBuffer(make([]byte, 0, 4096))

	parts := strings.Split(apropos, ".", 2)
	switch len(parts) {
	case 1:
		// propose modules
		for _, value := range self.cfns {
			if decl, ok := self.m[value]; ok {
				self.appendDecl(out_names, out_types, out_classes, parts[0], decl)
			}
		}
		// and locals
		for _, value := range self.l {
			value.InferType(self)
			self.appendDecl(out_names, out_types, out_classes, parts[0], value)
		}
	case 2:
		if topdecl := self.findDecl(parts[0]); topdecl != nil {
			switch topdecl.Class {
			case DECL_MODULE:
				for _, decl := range topdecl.Children {
					self.appendDecl(out_names, out_types, out_classes, parts[1], decl)
				}
			case DECL_VAR:
				it := topdecl.InferType(self)
				name := typePath(it)
				if typdecl := self.findDeclByPath(name); typdecl != nil {
					for _, decl := range typdecl.Children {
						self.appendDecl(out_names, out_types, out_classes, parts[1], decl)
					}
				}
			case DECL_TYPE:
				for _, decl := range topdecl.Children {
					self.appendDecl(out_names, out_types, out_classes, parts[1], decl)
				}
			}
		}
	}

	if out_names.Len() == 0 || out_types.Len() == 0 || out_classes.Len() == 0 {
		return nil, nil, nil
	}

	var tri TriStringArrays
	tri.first = strings.Split(out_names.String()[0:out_names.Len()-1], "\n", -1)
	tri.second = strings.Split(out_types.String()[0:out_types.Len()-1], "\n", -1)
	tri.third = strings.Split(out_classes.String()[0:out_classes.Len()-1], "\n", -1)
	sort.Sort(tri)
	return tri.first, tri.second, tri.third
}

func (self *AutoCompleteContext) Status() string {
	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	fmt.Fprintf(buf, "Number of top level packages: %d\n", len(self.m))
	if len(self.m) > 0 {
		fmt.Fprintf(buf, "Listing packages: ")
		i := 0
		for key, _ := range self.m {
			fmt.Fprintf(buf, "'%s'", key)
			if i != len(self.m)-1 {
				fmt.Fprintf(buf, ", ")
			}
			i++
		}
		fmt.Fprintf(buf, "\n")
	}
	return buf.String()
}
