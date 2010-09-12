package main

import (
	"go/parser"
	"go/ast"
	"go/token"
)

//-------------------------------------------------------------------------
// AutoCompleteFile
//-------------------------------------------------------------------------

type AutoCompleteFile struct {
	name        string
	packageName string

	decls     map[string]*Decl
	packages  PackageImports
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

// this one is used for current file buffer exclusively
func (f *AutoCompleteFile) processData(data []byte) {
	cur, filedata, block := RipOffDecl(data, f.cursor)
	file, _ := parser.ParseFile("", filedata, 0)
	f.packageName = packageName(file)

	f.decls = make(map[string]*Decl)
	f.packages = NewPackageImports(f.name, file.Decls)
	f.filescope = NewScope(nil)
	f.scope = f.filescope

	anonymifyAst(file.Decls, 0, f.filescope)

	// process all top-level declarations
	for _, decl := range file.Decls {
		appendToTopDecls(f.decls, decl, f.scope)
	}
	if block != nil {
		// process local function as top-level declaration
		decls, _ := parser.ParseDeclList("", block)

		anonymifyAst(decls, 0, f.filescope)

		for _, decl := range decls {
			appendToTopDecls(f.decls, decl, f.scope)
		}

		// process function internals
		f.cursor = cur
		for _, decl := range decls {
			f.processDeclLocals(decl)
		}
	}

}

func (f *AutoCompleteFile) processDeclLocals(decl ast.Decl) {
	switch t := decl.(type) {
	case *ast.FuncDecl:
		if f.cursorIn(t.Body) {
			s := f.scope
			f.scope = NewScope(f.scope)

			f.processFieldList(t.Recv, s)
			f.processFieldList(t.Type.Params, s)
			f.processFieldList(t.Type.Results, s)
			f.processBlockStmt(t.Body)

		}
	}
}

func (f *AutoCompleteFile) processDecl(decl ast.Decl) {
	if t, ok := decl.(*ast.GenDecl); ok && t.Offset > f.cursor {
		return
	}
	foreachDecl(decl, func(data *foreachDeclStruct) {
		class := astDeclClass(data.decl)
		for i, name := range data.names {
			typ, v, vi := data.typeValueIndex(i, 0)

			d := NewDecl2(name.Name, class, 0, typ, v, vi, f.scope)
			if d == nil {
				return
			}

			// TODO: The code here is incorrect, but I wasn't able to break
			// it. Maybe it is correct for autocompletion case.
			if d.Class != DECL_TYPE {
				f.scope = NewScope(f.scope)
			}
			f.scope.addNamedDecl(d)
		}
	})
}

func (f *AutoCompleteFile) processBlockStmt(block *ast.BlockStmt) {
	if block != nil && f.cursorIn(block) {
		f.scope = AdvanceScope(f.scope)

		for _, stmt := range block.List {
			f.processStmt(stmt)
		}

		// hack to process all func literals
		v := new(funcLitVisitor)
		v.ctx = f
		ast.Walk(v, block)
	}
}

type funcLitVisitor struct {
	ctx *AutoCompleteFile
}

func (v *funcLitVisitor) Visit(node interface{}) ast.Visitor {
	if t, ok := node.(*ast.FuncLit); ok && v.ctx.cursorIn(t.Body) {
		s := v.ctx.scope
		v.ctx.scope = AdvanceScope(v.ctx.scope)

		v.ctx.processFieldList(t.Type.Params, s)
		v.ctx.processFieldList(t.Type.Results, s)
		v.ctx.processBlockStmt(t.Body)

		return nil
	}
	return v
}

func (f *AutoCompleteFile) processStmt(stmt ast.Stmt) {
	switch t := stmt.(type) {
	case *ast.DeclStmt:
		f.processDecl(t.Decl)
	case *ast.AssignStmt:
		f.processAssignStmt(t)
	case *ast.IfStmt:
		if f.cursorIn(t.Body) {
			f.scope = AdvanceScope(f.scope)

			f.processStmt(t.Init)
			f.processBlockStmt(t.Body)
		}
		f.processStmt(t.Else)
	case *ast.BlockStmt:
		f.processBlockStmt(t)
	case *ast.RangeStmt:
		f.processRangeStmt(t)
	case *ast.ForStmt:
		if f.cursorIn(t.Body) {
			f.scope = AdvanceScope(f.scope)

			f.processStmt(t.Init)
			f.processBlockStmt(t.Body)
		}
	case *ast.SwitchStmt:
		f.processSwitchStmt(t)
	case *ast.TypeSwitchStmt:
		f.processTypeSwitchStmt(t)
	case *ast.SelectStmt:
		f.processSelectStmt(t)
	case *ast.LabeledStmt:
		f.processStmt(t.Stmt)
	}
}

func (f *AutoCompleteFile) processSelectStmt(a *ast.SelectStmt) {
	if !f.cursorIn(a.Body) {
		return
	}
	f.scope = AdvanceScope(f.scope)

	var lastCursorAfter *ast.CommClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.CommClause); f.cursor > cc.Colon.Offset {
			lastCursorAfter = cc
		}
	}

	if lastCursorAfter != nil {
		if lastCursorAfter.Lhs != nil && lastCursorAfter.Tok == token.DEFINE {
			vname := lastCursorAfter.Lhs.(*ast.Ident).Name
			v := NewDeclVar(vname, nil, lastCursorAfter.Rhs, -1, f.scope)
			f.scope.addNamedDecl(v)
		}
		for _, s := range lastCursorAfter.Body {
			f.processStmt(s)
		}
	}
}

func (f *AutoCompleteFile) processTypeSwitchStmt(a *ast.TypeSwitchStmt) {
	if !f.cursorIn(a.Body) {
		return
	}
	f.scope = AdvanceScope(f.scope)

	f.processStmt(a.Init)
	// type var
	var tv *Decl
	if a, ok := a.Assign.(*ast.AssignStmt); ok {
		lhs := a.Lhs
		rhs := a.Rhs
		if lhs != nil && len(lhs) == 1 {
			tvname := lhs[0].(*ast.Ident).Name
			tv = NewDeclVar(tvname, nil, rhs[0], -1, f.scope)
		}
	}

	var lastCursorAfter *ast.TypeCaseClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.TypeCaseClause); f.cursor > cc.Colon.Offset {
			lastCursorAfter = cc
		}
	}

	if lastCursorAfter != nil {
		if tv != nil {
			if lastCursorAfter.Types != nil && len(lastCursorAfter.Types) == 1 {
				tv.Type = lastCursorAfter.Types[0]
				tv.Value = nil
			}
			f.scope.addNamedDecl(tv)
		}
		for _, s := range lastCursorAfter.Body {
			f.processStmt(s)
		}
	}
}

func (f *AutoCompleteFile) processSwitchStmt(a *ast.SwitchStmt) {
	if !f.cursorIn(a.Body) {
		return
	}
	f.scope = AdvanceScope(f.scope)

	f.processStmt(a.Init)
	var lastCursorAfter *ast.CaseClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.CaseClause); f.cursor > cc.Colon.Offset {
			lastCursorAfter = cc
		}
	}
	if lastCursorAfter != nil {
		for _, s := range lastCursorAfter.Body {
			f.processStmt(s)
		}
	}
}

func (f *AutoCompleteFile) processRangeStmt(a *ast.RangeStmt) {
	if !f.cursorIn(a.Body) {
		return
	}
	f.scope = AdvanceScope(f.scope)

	if a.Tok == token.DEFINE {
		if t, ok := a.Key.(*ast.Ident); ok {
			d := NewDeclVar(t.Name, nil, a.X, 0, f.scope)
			if d != nil {
				d.Flags |= DECL_RANGEVAR
				f.scope.addNamedDecl(d)
			}
		}

		if a.Value != nil {
			if t, ok := a.Value.(*ast.Ident); ok {
				d := NewDeclVar(t.Name, nil, a.X, 1, f.scope)
				if d != nil {
					d.Flags |= DECL_RANGEVAR
					f.scope.addNamedDecl(d)
				}
			}
		}
	}

	f.processBlockStmt(a.Body)
}

func (f *AutoCompleteFile) processAssignStmt(a *ast.AssignStmt) {
	if a.Tok != token.DEFINE || a.TokPos.Offset > f.cursor {
		return
	}

	names := make([]*ast.Ident, len(a.Lhs))
	for i, name := range a.Lhs {
		id, ok := name.(*ast.Ident)
		if !ok {
			// something is wrong, just ignore the whole stmt
			return
		}
		names[i] = id
	}

	pack := declPack{names, nil, a.Rhs}
	for i, name := range pack.names {
		typ, v, vi := pack.typeValueIndex(i, 0)
		d := NewDeclVar(name.Name, typ, v, vi, f.scope)
		if d == nil {
			continue
		}

		f.scope = NewScope(f.scope)
		f.scope.addNamedDecl(d)
	}
}

func (f *AutoCompleteFile) processFieldList(fieldList *ast.FieldList, s *Scope) {
	if fieldList != nil {
		decls := astFieldListToDecls(fieldList, DECL_VAR, 0, s)
		for _, d := range decls {
			f.scope.addNamedDecl(d)
		}
	}
}

func (f *AutoCompleteFile) cursorIn(block *ast.BlockStmt) bool {
	if f.cursor == -1 || block == nil {
		return false
	}

	if f.cursor >= block.Offset && f.cursor <= block.Rbrace.Offset {
		return true
	}
	return false
}
