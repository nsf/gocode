package main

import (
	"os"
	"go/parser"
	"go/ast"
	"go/token"
)

type AutoCompleteFile struct {
	commonFile

	decls  map[string]*Decl // map of all top-level declarations (cached)
	cursor int              // for current file buffer only
}

func NewPackageFile(name, packageName string) *AutoCompleteFile {
	p := new(AutoCompleteFile)
	p.name = name
	p.packageName = packageName
	p.mtime = 0
	p.cursor = -1
	return p
}

func (self *AutoCompleteFile) updateCache(c *ASTCache) {
	stat, err := os.Stat(self.name)
	if err != nil {
		panic(err.String())
	}

	if self.mtime != stat.Mtime_ns {
		self.mtime = stat.Mtime_ns
		self.processFile(self.name, c)
	}
}

func (self *AutoCompleteFile) processFile(filename string, c *ASTCache) {
	// drop cached modules and file scope
	self.resetCache()

	file, _, _ := c.ForceGet(filename, self.mtime)
	self.processImports(file.Decls)

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
func (self *AutoCompleteFile) processDataStage1(data []byte, ctx *ProcessDataContext) {
	self.resetCache()

	var filedata []byte
	ctx.cur, filedata, ctx.block = RipOffDecl(data, self.cursor)

	// process file without locals first
	ctx.file, _ = parser.ParseFile("", filedata, 0)

	// STAGE 1
	self.packageName = packageName(ctx.file)
	self.processImports(ctx.file.Decls)

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

// this one is used for current file buffer exclusively
func (self *AutoCompleteFile) processDataStage2(ctx *ProcessDataContext) {
	// STAGE 2 for current file buffer only
	if ctx.block != nil {
		// parse local function as local
		self.cursor = ctx.cur
		for _, decl := range ctx.decls {
			self.processDeclLocals(decl)
		}
	}
}

func (self *AutoCompleteFile) resetCache() {
	self.commonFile.resetCache()
	self.decls = make(map[string]*Decl)
}

func (self *AutoCompleteFile) processDeclLocals(decl ast.Decl) {
	switch t := decl.(type) {
	case *ast.FuncDecl:
		if self.cursorIn(t.Body) {
			self.scope = NewScope(self.scope)

			self.processFieldList(t.Recv)
			self.processFieldList(t.Type.Params)
			self.processFieldList(t.Type.Results)
			self.processBlockStmt(t.Body)

		}
	}
}

func (self *AutoCompleteFile) processDecl(decl ast.Decl) {
	if self.scope != self.filescope {
		if t, ok := decl.(*ast.GenDecl); ok && t.Offset > self.cursor {
			return
		}
	}
	foreachDecl(decl, func(decl ast.Decl, name string, value ast.Expr, valueindex int) {
		d := NewDeclFromAstDecl(name, 0, decl, value, valueindex, self.scope)
		if d == nil {
			return
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
				}
				self.scope.addNamedDecl(d)
			} else {
				self.addVarDecl(d)
			}
		}
	})
}

func (self *AutoCompleteFile) processBlockStmt(block *ast.BlockStmt) {
	if block != nil && self.cursorIn(block) {
		self.scope = AdvanceScope(self.scope)

		for _, stmt := range block.List {
			self.processStmt(stmt)
		}

		// hack to process all func literals
		v := new(funcLitVisitor)
		v.ctx = self
		ast.Walk(v, block)
	}
}

type funcLitVisitor struct {
	ctx *AutoCompleteFile
}

func (v *funcLitVisitor) Visit(node interface{}) ast.Visitor {
	if t, ok := node.(*ast.FuncLit); ok && v.ctx.cursorIn(t.Body) {
		v.ctx.scope = AdvanceScope(v.ctx.scope)

		v.ctx.processFieldList(t.Type.Params)
		v.ctx.processFieldList(t.Type.Results)
		v.ctx.processBlockStmt(t.Body)

		return nil
	}
	return v
}

func (self *AutoCompleteFile) processStmt(stmt ast.Stmt) {
	switch t := stmt.(type) {
	case *ast.DeclStmt:
		self.processDecl(t.Decl)
	case *ast.AssignStmt:
		self.processAssignStmt(t)
	case *ast.IfStmt:
		if self.cursorIn(t.Body) {
			self.scope = AdvanceScope(self.scope)

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
			self.scope = AdvanceScope(self.scope)

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

func (self *AutoCompleteFile) processSelectStmt(a *ast.SelectStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	self.scope = AdvanceScope(self.scope)

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
}

func (self *AutoCompleteFile) processTypeSwitchStmt(a *ast.TypeSwitchStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	self.scope = AdvanceScope(self.scope)

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
}

func (self *AutoCompleteFile) processSwitchStmt(a *ast.SwitchStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	self.scope = AdvanceScope(self.scope)

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

func (self *AutoCompleteFile) processRangeStmt(a *ast.RangeStmt) {
	if !self.cursorIn(a.Body) {
		return
	}
	self.scope = AdvanceScope(self.scope)

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
}

func (self *AutoCompleteFile) processAssignStmt(a *ast.AssignStmt) {
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

func (self *AutoCompleteFile) processFieldList(fieldList *ast.FieldList) {
	if fieldList != nil {
		decls := astFieldListToDecls(fieldList, DECL_VAR, 0, self.scope)
		for _, d := range decls {
			self.scope.addNamedDecl(d)
		}
	}
}

func (self *AutoCompleteFile) addVarDecl(d *Decl) {
	decl, ok := self.decls[d.Name]
	if ok {
		decl.ExpandOrReplace(d)
	} else {
		self.decls[d.Name] = d
	}
}


func (self *AutoCompleteFile) cursorIn(block *ast.BlockStmt) bool {
	if self.cursor == -1 || block == nil {
		return false
	}

	if self.cursor >= block.Offset && self.cursor <= block.Rbrace.Offset {
		return true
	}
	return false
}

