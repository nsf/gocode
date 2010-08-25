package main

import (
	"os"
	"go/parser"
	"go/ast"
	"go/token"
	"path"
	"fmt"
)

type moduleImport struct {
	alias  string
	name   string
	path   string
	module *Decl
}

type PackageFile struct {
	name        string // file name
	packageName string // package name that is in the file
	mtime       int64

	modules   []moduleImport   // import section cache (abbrev -> full name)
	decls     map[string]*Decl // cached
	filescope *Scope           // cached

	topscope *Scope
	scope    *Scope // scope, used for parsing
	cursor   int    // for current file buffer

	stage2go chan bool
}

func NewPackageFile(name, packageName string) *PackageFile {
	p := new(PackageFile)
	p.name = name
	p.packageName = packageName
	p.mtime = 0
	p.cursor = -1
	p.stage2go = make(chan bool)
	return p
}

func (self *PackageFile) updateCache(stage1 chan *PackageFile, stage2 chan bool) {
	stat, err := os.Stat(self.name)
	if err != nil {
		panic(err.String())
	}

	if self.mtime != stat.Mtime_ns {
		self.processFile(self.name, stage1)
		self.mtime = stat.Mtime_ns
	} else {
		stage1 <- self
		<-self.stage2go
	}
	stage2 <- true
}

func (self *PackageFile) processFile(filename string, stage1 chan *PackageFile) {
	// drop cached modules and file scope
	self.resetCache()

	file, _ := parser.ParseFile(filename, nil, 0)
	// STAGE 1
	// update import statements
	self.processImports(file.Decls)
	stage1 <- self
	<-self.stage2go

	// STAGE 2
	self.applyImports()
	// process declarations
	self.decls = make(map[string]*Decl, len(file.Decls))
	for _, decl := range file.Decls {
		self.processDecl(decl)
	}
}

type ProcessDataContext struct {
	cur   int
	block []byte
	file  *ast.File
	decls []ast.Decl
}

// this one is used for current file buffer exclusively
func (self *PackageFile) processDataStage1(data []byte, ctx *ProcessDataContext) {
	self.resetCache()

	var filedata []byte
	ctx.cur, filedata, ctx.block = RipOffDecl(data, self.cursor)

	// process file without locals first
	ctx.file, _ = parser.ParseFile("", filedata, 0)
	// STAGE 1
	self.packageName = packageName(ctx.file)
	self.processImports(ctx.file.Decls)
}

func (self *PackageFile) processDataStage2(ctx *ProcessDataContext) {
	// STAGE 2
	self.applyImports()
	for _, decl := range ctx.file.Decls {
		self.processDecl(decl)
	}
	if ctx.block != nil {
		// parse local function as global
		ctx.decls, _ = parser.ParseDeclList("", ctx.block)
		for _, decl := range ctx.decls {
			self.processDecl(decl)
		}
	}
}

func (self *PackageFile) processDataStage3(ctx *ProcessDataContext) {
	// STAGE 3 for current file buffer only
	if ctx.block != nil {
		// parse local function as local
		self.cursor = ctx.cur
		for _, decl := range ctx.decls {
			self.processDeclLocals(decl)
		}
	}
}

func (self *PackageFile) resetCache() {
	self.modules = make([]moduleImport, 0, 16)
	self.filescope = NewScope(nil)
	self.scope = self.filescope
	self.topscope = self.filescope
	self.decls = make(map[string]*Decl)
}

func (self *PackageFile) addModuleImport(alias, path string) {
	if alias == "_" || alias == "." {
		// TODO: support for modules imported in the current namespace
		return
	}
	name := path
	path = self.findFile(path)
	if name[0] == '.' {
		// use file path for local packages as name
		name = path
	}

	n := len(self.modules)
	if cap(self.modules) < n+1 {
		s := make([]moduleImport, n, n*2+1)
		copy(s, self.modules)
		self.modules = s
	}

	self.modules = self.modules[0 : n+1]
	self.modules[n] = moduleImport{alias, name, path, nil}
}

func (self *PackageFile) processImports(decls []ast.Decl) {
	for _, decl := range decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			for _, spec := range gd.Specs {
				imp, ok := spec.(*ast.ImportSpec)
				if !ok {
					panic("Fail")
				}
				self.processImportSpec(imp)
			}
		} else {
			return
		}
	}
}

func (self *PackageFile) applyImports() {
	for _, mi := range self.modules {
		self.filescope.addDecl(mi.alias, mi.module)
	}
}

func (self *PackageFile) processImportSpec(imp *ast.ImportSpec) {
	path, alias := pathAndAlias(imp)

	// add module to a cache
	self.addModuleImport(alias, path)
}

func (self *PackageFile) processDeclLocals(decl ast.Decl) {
	switch t := decl.(type) {
	case *ast.FuncDecl:
		if self.cursorIn(t.Body) {
			// put into 'locals' (if any):
			// 1. method var
			// 2. args vars
			// 3. results vars
			savescope := self.scope
			self.scope = NewScope(self.scope)
			self.topscope = self.scope

			self.processFieldList(t.Recv)
			self.processFieldList(t.Type.Params)
			self.processFieldList(t.Type.Results)
			self.processBlockStmt(t.Body)

			self.scope = savescope
		}
	}
}

func (self *PackageFile) processDecl(decl ast.Decl) {
	if self.scope != self.filescope {
		if t, ok := decl.(*ast.GenDecl); ok && t.Offset > self.cursor {
			return
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

			d := NewDeclFromAstDecl(name, 0, decl, value, valueindex, self.scope)
			if d == nil {
				continue
			}

			methodof := MethodOf(decl)
			if methodof != "" {
				decl, ok := self.decls[methodof]
				if ok {
					decl.AddChild(d)
				} else {
					decl = NewDecl(methodof, DECL_METHODS_STUB, self.scope)
					self.decls[methodof] = decl
					decl.AddChild(d)
				}
			} else {
				if self.scope != self.filescope {
					// the declaration itself has a scope which follows it's definition
					// and it's false for type declarations
					if d.Class != DECL_TYPE {
						self.scope = NewScope(self.scope)
						self.topscope = self.scope
					}
					self.scope.addNamedDecl(d)
				} else {
					self.addVarDecl(d)
				}
			}
		}
	}
}

func (self *PackageFile) processBlockStmt(block *ast.BlockStmt) {
	if block != nil && self.cursorIn(block) {
		savescope := self.scope
		self.scope = AdvanceScope(self.scope)
		self.topscope = self.scope

		for _, stmt := range block.List {
			self.processStmt(stmt)
		}

		// hack to process all func literals
		v := new(funcLitVisitor)
		v.ctx = self
		ast.Walk(v, block)

		self.scope = savescope
	}
}

type funcLitVisitor struct {
	ctx *PackageFile
}

func (v *funcLitVisitor) Visit(node interface{}) ast.Visitor {
	if t, ok := node.(*ast.FuncLit); ok && v.ctx.cursorIn(t.Body) {
		savescope := v.ctx.scope
		v.ctx.scope = AdvanceScope(v.ctx.scope)
		v.ctx.topscope = v.ctx.scope

		v.ctx.processFieldList(t.Type.Params)
		v.ctx.processFieldList(t.Type.Results)
		v.ctx.processBlockStmt(t.Body)

		v.ctx.scope = savescope
		return nil
	}
	return v
}

func (self *PackageFile) processStmt(stmt ast.Stmt) {
	switch t := stmt.(type) {
	case *ast.DeclStmt:
		self.processDecl(t.Decl)
	case *ast.AssignStmt:
		self.processAssignStmt(t)
	case *ast.IfStmt:
		if self.cursorIn(t.Body) {
			savescope := self.scope
			self.scope = AdvanceScope(self.scope)
			self.topscope = self.scope

			self.processStmt(t.Init)
			self.processBlockStmt(t.Body)

			self.scope = savescope
		}
		self.processStmt(t.Else)
	case *ast.BlockStmt:
		self.processBlockStmt(t)
	case *ast.RangeStmt:
		self.processRangeStmt(t)
	case *ast.ForStmt:
		if self.cursorIn(t.Body) {
			savescope := self.scope
			self.scope = AdvanceScope(self.scope)
			self.topscope = self.scope

			self.processStmt(t.Init)
			self.processBlockStmt(t.Body)

			self.scope = savescope
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

func (self *PackageFile) processSelectStmt(a *ast.SelectStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	savescope := self.scope
	self.scope = AdvanceScope(self.scope)
	self.topscope = self.scope

	var lastCursorAfter *ast.CommClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.CommClause); self.cursor > cc.Colon.Offset {
			lastCursorAfter = cc
		}
	}

	if lastCursorAfter != nil {
		if lastCursorAfter.Lhs != nil && lastCursorAfter.Tok == token.DEFINE {
			vname := lastCursorAfter.Lhs.(*ast.Ident).Name
			v := NewDeclVar(vname, nil, lastCursorAfter.Rhs, -1, self.scope)
			self.scope.addNamedDecl(v)
		}
		for _, s := range lastCursorAfter.Body {
			self.processStmt(s)
		}
	}

	self.scope = savescope
}

func (self *PackageFile) processTypeSwitchStmt(a *ast.TypeSwitchStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	savescope := self.scope
	self.scope = AdvanceScope(self.scope)
	self.topscope = self.scope

	self.processStmt(a.Init)
	// type var
	var tv *Decl
	if a, ok := a.Assign.(*ast.AssignStmt); ok {
		lhs := a.Lhs
		rhs := a.Rhs
		if lhs != nil && len(lhs) == 1 {
			tvname := lhs[0].(*ast.Ident).Name
			tv = NewDeclVar(tvname, nil, rhs[0], -1, self.scope)
		}
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
				tv.Value = nil
			}
			self.scope.addNamedDecl(tv)
		}
		for _, s := range lastCursorAfter.Body {
			self.processStmt(s)
		}
	}

	self.scope = savescope
}

func (self *PackageFile) processSwitchStmt(a *ast.SwitchStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	savescope := self.scope
	self.scope = AdvanceScope(self.scope)
	self.topscope = self.scope

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

	self.scope = savescope
}

func (self *PackageFile) processRangeStmt(a *ast.RangeStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	savescope := self.scope
	self.scope = AdvanceScope(self.scope)
	self.topscope = self.scope

	if a.Tok == token.DEFINE {
		var t1, t2 ast.Expr
		var scope *Scope
		t1, scope = NewDeclVar("tmp", nil, a.X, -1, self.scope).InferType()
		t1, scope = advanceToType(rangePredicate, t1, scope)
		if t1 != nil {
			// figure out range Key, Value types
			switch t := t1.(type) {
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

			if t, ok := a.Key.(*ast.Ident); ok {
				d := NewDeclVar(t.Name, t1, nil, -1, self.scope)
				if d != nil {
					self.scope.addNamedDecl(d)
				}
			}

			if a.Value != nil {
				if t, ok := a.Value.(*ast.Ident); ok {
					d := NewDeclVar(t.Name, t2, nil, -1, self.scope)
					if d != nil {
						self.scope.addNamedDecl(d)
					}
				}
			}
		}
	}

	self.processBlockStmt(a.Body)

	self.scope = savescope
}

func (self *PackageFile) processAssignStmt(a *ast.AssignStmt) {
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
		names[i] = id.Name
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

		d := NewDeclVar(name, nil, value, valueindex, self.scope)
		if d == nil {
			continue
		}

		self.scope.addNamedDecl(d)
	}
}

func (self *PackageFile) processFieldList(fieldList *ast.FieldList) {
	if fieldList != nil {
		decls := astFieldListToDecls(fieldList, DECL_VAR, 0, self.scope)
		for _, d := range decls {
			self.scope.addNamedDecl(d)
		}
	}
}

func (self *PackageFile) addVarDecl(d *Decl) {
	decl, ok := self.decls[d.Name]
	if ok {
		decl.ExpandOrReplace(d)
	} else {
		self.decls[d.Name] = d
	}
}


func (self *PackageFile) cursorIn(block *ast.BlockStmt) bool {
	if self.cursor == -1 || block == nil {
		return false
	}

	if self.cursor >= block.Offset && self.cursor <= block.Rbrace.Offset {
		return true
	}
	return false
}

func (self *PackageFile) findFile(imp string) string {
	if imp[0] == '.' {
		dir, _ := path.Split(self.name)
		return fmt.Sprintf("%s.a", path.Join(dir, imp))
	}
	return findGlobalFile(imp)
}

func packageName(file *ast.File) string {
	if file.Name != nil {
		return file.Name.Name
	}
	return ""
}

func pathAndAlias(imp *ast.ImportSpec) (string, string) {
	path := string(imp.Path.Value)
	alias := ""
	if imp.Name != nil {
		alias = imp.Name.Name
	}
	path = path[1 : len(path)-1]
	return path, alias
}

func findGlobalFile(imp string) string {
	goroot := os.Getenv("GOROOT")
	goarch := os.Getenv("GOARCH")
	goos := os.Getenv("GOOS")

	pkgdir := fmt.Sprintf("%s_%s", goos, goarch)
	pkgfile := fmt.Sprintf("%s.a", imp)

	return path.Join(goroot, "pkg", pkgdir, pkgfile)
}
