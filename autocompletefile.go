package main

import (
	"go/parser"
	"go/ast"
	"go/token"
)

type AutoCompleteFile struct {
	name        string
	packageName string

	decls     map[string]*Decl
	modules   ModuleImports
	filescope *Scope
	scope     *Scope

	cursor int // for current file buffer only
}

func NewPackageFile(name string) *AutoCompleteFile {
	p := new(AutoCompleteFile)
	p.name = name
	p.cursor = -1
	return p
}

func (self *AutoCompleteFile) updateCache(c *DeclCache) {
	cache := c.Get(self.name)
	self.packageName = packageName(cache.File)

	self.decls = cache.Decls
	self.modules = cache.Modules
	self.filescope = cache.FileScope
	self.scope = self.filescope
}

// this one is used for current file buffer exclusively
func (self *AutoCompleteFile) processData(data []byte) {
	cur, filedata, block := RipOffDecl(data, self.cursor)
	file, _ := parser.ParseFile("", filedata, 0)
	self.packageName = packageName(file)

	self.decls = make(map[string]*Decl)
	self.modules = NewModuleImports(self.name, file.Decls)
	self.filescope = NewScope(nil)
	self.scope = self.filescope

	// process all top-level declarations
	for _, decl := range file.Decls {
		appendToTopDecls(self.decls, decl, self.scope)
	}
	if block != nil {
		// process local function as top-level declaration
		decls, _ := parser.ParseDeclList("", block)
		for _, decl := range decls {
			appendToTopDecls(self.decls, decl, self.scope)
		}

		// process function internals
		self.cursor = cur
		for _, decl := range decls {
			self.processDeclLocals(decl)
		}
	}

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
	if t, ok := decl.(*ast.GenDecl); ok && t.Offset > self.cursor {
		return
	}
	foreachDecl(decl, func(decl ast.Decl, name *ast.Ident, value ast.Expr, valueindex int) {
		d := NewDeclFromAstDecl(name.Name, 0, decl, value, valueindex, self.scope)
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
			// the declaration itself has a scope which follows it's definition
			// and it's false for type declarations
			if d.Class != DECL_TYPE {
				self.scope = NewScope(self.scope)
			}
			self.scope.addNamedDecl(d)
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
		if t, ok := a.Key.(*ast.Ident); ok {
			d := NewDeclVar(t.Name, nil, a.X, 0, self.scope)
			if d != nil {
				d.Flags |= DECL_RANGEVAR
				self.scope.addNamedDecl(d)
			}
		}

		if a.Value != nil {
			if t, ok := a.Value.(*ast.Ident); ok {
				d := NewDeclVar(t.Name, nil, a.X, 1, self.scope)
				if d != nil {
					d.Flags |= DECL_RANGEVAR
					self.scope.addNamedDecl(d)
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

func (self *AutoCompleteFile) cursorIn(block *ast.BlockStmt) bool {
	if self.cursor == -1 || block == nil {
		return false
	}

	if self.cursor >= block.Offset && self.cursor <= block.Rbrace.Offset {
		return true
	}
	return false
}

func (self *AutoCompleteFile) applyImports() {
	for _, mi := range self.modules {
		self.filescope.addDecl(mi.Alias, mi.Module)
	}
}
