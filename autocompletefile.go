package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
)

func parseDeclList(fset *token.FileSet, data []byte) ([]ast.Decl, error) {
	var buf bytes.Buffer
	buf.WriteString("package p;")
	buf.Write(data)
	file, err := parser.ParseFile(fset, "", buf.Bytes(), 0)
	if err != nil {
		return file.Decls, err
	}
	return file.Decls, nil
}

//-------------------------------------------------------------------------
// autoCompleteFile
//-------------------------------------------------------------------------

type autoCompleteFile struct {
	name         string
	packageName string

	decls     map[string]*decl
	packages  []packageImport
	filescope *scope
	scope     *scope

	cursor int // for current file buffer only
	fset   *token.FileSet
	env    *gocodeEnv
}

func newAutoCompleteFile(name string, env *gocodeEnv) *autoCompleteFile {
	p := new(autoCompleteFile)
	p.name = name
	p.cursor = -1
	p.fset = token.NewFileSet()
	p.env = env
	return p
}

func (f *autoCompleteFile) offset(p token.Pos) int {
	const fixlen = len("package p;")
	return f.fset.Position(p).Offset - fixlen
}

// this one is used for current file buffer exclusively
func (f *autoCompleteFile) processData(data []byte) {
	cur, filedata, block := ripOffDecl(data, f.cursor)
	file, _ := parser.ParseFile(f.fset, "", filedata, 0)
	f.packageName = packageName(file)

	f.decls = make(map[string]*decl)
	f.packages = collectPackageImports(f.name, file.Decls, f.env)
	f.filescope = newScope(nil)
	f.scope = f.filescope

	for _, d := range file.Decls {
		anonymifyAst(d, 0, f.filescope)
	}

	// process all top-level declarations
	for _, decl := range file.Decls {
		appendToTopDecls(f.decls, decl, f.scope)
	}
	if block != nil {
		// process local function as top-level declaration
		decls, _ := parseDeclList(f.fset, block)

		for _, d := range decls {
			anonymifyAst(d, 0, f.filescope)
		}

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

func (f *autoCompleteFile) processDeclLocals(decl ast.Decl) {
	switch t := decl.(type) {
	case *ast.FuncDecl:
		if f.cursorIn(t.Body) {
			s := f.scope
			f.scope = newScope(f.scope)

			f.processFieldList(t.Recv, s)
			f.processFieldList(t.Type.Params, s)
			f.processFieldList(t.Type.Results, s)
			f.processBlockStmt(t.Body)

		}
	default:
		v := new(funcLitVisitor)
		v.ctx = f
		ast.Walk(v, decl)
	}
}

func (f *autoCompleteFile) processDecl(decl ast.Decl) {
	if t, ok := decl.(*ast.GenDecl); ok && f.offset(t.TokPos) > f.cursor {
		return
	}
	prevscope := f.scope
	foreachDecl(decl, func(data *foreachDeclStruct) {
		class := astDeclClass(data.decl)
		if class != declType {
			f.scope, prevscope = advanceScope(f.scope)
		}
		for i, name := range data.names {
			typ, v, vi := data.typeValueIndex(i)

			d := newDeclFull(name.Name, class, 0, typ, v, vi, prevscope)
			if d == nil {
				return
			}

			f.scope.addNamedDecl(d)
		}
	})
}

func (f *autoCompleteFile) processBlockStmt(block *ast.BlockStmt) {
	if block != nil && f.cursorIn(block) {
		f.scope, _ = advanceScope(f.scope)

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
	ctx *autoCompleteFile
}

func (v *funcLitVisitor) Visit(node ast.Node) ast.Visitor {
	if t, ok := node.(*ast.FuncLit); ok && v.ctx.cursorIn(t.Body) {
		s := v.ctx.scope
		v.ctx.scope = newScope(v.ctx.scope)

		v.ctx.processFieldList(t.Type.Params, s)
		v.ctx.processFieldList(t.Type.Results, s)
		v.ctx.processBlockStmt(t.Body)

		return nil
	}
	return v
}

func (f *autoCompleteFile) processStmt(stmt ast.Stmt) {
	switch t := stmt.(type) {
	case *ast.DeclStmt:
		f.processDecl(t.Decl)
	case *ast.AssignStmt:
		f.processAssignStmt(t)
	case *ast.IfStmt:
		if f.cursorInIfHead(t) {
			f.processStmt(t.Init)
		} else if f.cursorInIfStmt(t) {
			f.scope, _ = advanceScope(f.scope)
			f.processStmt(t.Init)
			f.processBlockStmt(t.Body)
			f.processStmt(t.Else)
		}
	case *ast.BlockStmt:
		f.processBlockStmt(t)
	case *ast.RangeStmt:
		f.processRangeStmt(t)
	case *ast.ForStmt:
		if f.cursorInForHead(t) {
			f.processStmt(t.Init)
		} else if f.cursorIn(t.Body) {
			f.scope, _ = advanceScope(f.scope)

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

func (f *autoCompleteFile) processSelectStmt(a *ast.SelectStmt) {
	if !f.cursorIn(a.Body) {
		return
	}
	var prevscope *scope
	f.scope, prevscope = advanceScope(f.scope)

	var lastCursorAfter *ast.CommClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.CommClause); f.cursor > f.offset(cc.Colon) {
			lastCursorAfter = cc
		}
	}

	if lastCursorAfter != nil {
		if lastCursorAfter.Comm != nil {
			//if lastCursorAfter.Lhs != nil && lastCursorAfter.Tok == token.DEFINE {
			if astmt, ok := lastCursorAfter.Comm.(*ast.AssignStmt); ok && astmt.Tok == token.DEFINE {
				vname := astmt.Lhs[0].(*ast.Ident).Name
				v := newDeclVar(vname, nil, astmt.Rhs[0], -1, prevscope)
				f.scope.addNamedDecl(v)
			}
		}
		for _, s := range lastCursorAfter.Body {
			f.processStmt(s)
		}
	}
}

func (f *autoCompleteFile) processTypeSwitchStmt(a *ast.TypeSwitchStmt) {
	if !f.cursorIn(a.Body) {
		return
	}
	var prevscope *scope
	f.scope, prevscope = advanceScope(f.scope)

	f.processStmt(a.Init)
	// type var
	var tv *decl
	if a, ok := a.Assign.(*ast.AssignStmt); ok {
		lhs := a.Lhs
		rhs := a.Rhs
		if lhs != nil && len(lhs) == 1 {
			tvname := lhs[0].(*ast.Ident).Name
			tv = newDeclVar(tvname, nil, rhs[0], -1, prevscope)
		}
	}

	var lastCursorAfter *ast.CaseClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.CaseClause); f.cursor > f.offset(cc.Colon) {
			lastCursorAfter = cc
		}
	}

	if lastCursorAfter != nil {
		if tv != nil {
			if lastCursorAfter.List != nil && len(lastCursorAfter.List) == 1 {
				tv.typ = lastCursorAfter.List[0]
				tv.value = nil
			}
			f.scope.addNamedDecl(tv)
		}
		for _, s := range lastCursorAfter.Body {
			f.processStmt(s)
		}
	}
}

func (f *autoCompleteFile) processSwitchStmt(a *ast.SwitchStmt) {
	if !f.cursorIn(a.Body) {
		return
	}
	f.scope, _ = advanceScope(f.scope)

	f.processStmt(a.Init)
	var lastCursorAfter *ast.CaseClause
	for _, s := range a.Body.List {
		if cc := s.(*ast.CaseClause); f.cursor > f.offset(cc.Colon) {
			lastCursorAfter = cc
		}
	}
	if lastCursorAfter != nil {
		for _, s := range lastCursorAfter.Body {
			f.processStmt(s)
		}
	}
}

func (f *autoCompleteFile) processRangeStmt(a *ast.RangeStmt) {
	if !f.cursorIn(a.Body) {
		return
	}
	var prevscope *scope
	f.scope, prevscope = advanceScope(f.scope)

	if a.Tok == token.DEFINE {
		if t, ok := a.Key.(*ast.Ident); ok {
			d := newDeclVar(t.Name, nil, a.X, 0, prevscope)
			if d != nil {
				d.flags |= declRangevar
				f.scope.addNamedDecl(d)
			}
		}

		if a.Value != nil {
			if t, ok := a.Value.(*ast.Ident); ok {
				d := newDeclVar(t.Name, nil, a.X, 1, prevscope)
				if d != nil {
					d.flags |= declRangevar
					f.scope.addNamedDecl(d)
				}
			}
		}
	}

	f.processBlockStmt(a.Body)
}

func (f *autoCompleteFile) processAssignStmt(a *ast.AssignStmt) {
	if a.Tok != token.DEFINE || f.offset(a.TokPos) > f.cursor {
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

	var prevscope *scope
	f.scope, prevscope = advanceScope(f.scope)

	pack := declPack{names, nil, a.Rhs}
	for i, name := range pack.names {
		typ, v, vi := pack.typeValueIndex(i)
		d := newDeclVar(name.Name, typ, v, vi, prevscope)
		if d == nil {
			continue
		}

		f.scope.addNamedDecl(d)
	}
}

func (f *autoCompleteFile) processFieldList(fieldList *ast.FieldList, s *scope) {
	if fieldList != nil {
		decls := astFieldListToDecls(fieldList, declVar, 0, s, false)
		for _, d := range decls {
			f.scope.addNamedDecl(d)
		}
	}
}

func (f *autoCompleteFile) cursorInIfHead(s *ast.IfStmt) bool {
	if f.cursor > f.offset(s.If) && f.cursor <= f.offset(s.Body.Lbrace) {
		return true
	}
	return false
}

func (f *autoCompleteFile) cursorInIfStmt(s *ast.IfStmt) bool {
	if f.cursor > f.offset(s.If) {
		// magic -10 comes from autoCompleteFile.offset method, see
		// len() expr in there
		if f.offset(s.End()) == -10 || f.cursor < f.offset(s.End()) {
			return true
		}
	}
	return false
}

func (f *autoCompleteFile) cursorInForHead(s *ast.ForStmt) bool {
	if f.cursor > f.offset(s.For) && f.cursor <= f.offset(s.Body.Lbrace) {
		return true
	}
	return false
}

func (f *autoCompleteFile) cursorIn(block *ast.BlockStmt) bool {
	if f.cursor == -1 || block == nil {
		return false
	}

	if f.cursor > f.offset(block.Lbrace) && f.cursor <= f.offset(block.Rbrace) {
		return true
	}
	return false
}
