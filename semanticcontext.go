package main

import (
	"go/ast"
	"go/token"
	"fmt"
)

type SemanticEntry struct {
	offset int
	length int
	line int
	col int
	decl *Decl
}

//-------------------------------------------------------------------------
// SemanticFile
//-------------------------------------------------------------------------

type SemanticFile struct {
	name string
	data []byte
	entries []SemanticEntry
	file *ast.File
	filescope *Scope

	scope *Scope
	block *Scope // that one is used temporary for 'redeclarations' case
}

func NewSemanticFile(dc *DeclFileCache) *SemanticFile {
	s := new(SemanticFile)
	s.name = dc.name
	s.data = dc.Data
	s.entries = make([]SemanticEntry, 0, 16)
	s.file = dc.File
	s.filescope = dc.FileScope
	s.scope = s.filescope
	return s
}

func (s *SemanticFile) appendEntry(offset, length, line, col int, decl *Decl) {
	n := len(s.entries)
	if cap(s.entries) < n+1 {
		e := make([]SemanticEntry, n, n*2+1)
		copy(e, s.entries)
		s.entries = e
	}

	s.entries = s.entries[0 : n+1]
	s.entries[n] = SemanticEntry{offset, length, line, col, decl}
}

func (s *SemanticFile) semantifyIdentFor(e *ast.Ident, d *Decl) *Decl {
	c := d.FindChildAndInEmbedded(e.Name)
	if c == nil {
		msg := fmt.Sprintf("Cannot resolve '%s' symbol at %s:%d",
				   e.Name, s.name, e.Line)
		panic(msg)
	}
	s.appendEntry(e.Offset, len(e.Name), e.Line, e.Column, c)
	return c
}

func (s *SemanticFile) semantifyTypeFor(e ast.Expr, d *Decl) {
	if d.Class == DECL_VAR {
		// vars don't have children, find its type
		typ, scope := d.InferType()
		d = typeToDecl(typ, scope)
	}
	switch t := e.(type) {
	case *ast.StructType:
		s.semantifyFieldListFor(t.Fields, d)
	case *ast.InterfaceType:
		s.semantifyFieldListFor(t.Methods, d)
	default:
		s.semantifyExpr(e)
	}
}

func (s *SemanticFile) semantifyValueFor(e ast.Expr, d *Decl, scope *Scope) {
	savescope := s.scope
	s.scope = scope
	switch t := e.(type) {
	case *ast.CompositeLit:
		s.semantifyTypeFor(t.Type, d)
		for _, e := range t.Elts {
			s.semantifyExpr(e)
		}
	default:
		s.semantifyExpr(e)
	}
	s.scope = savescope
}

func (s *SemanticFile) semantifyExpr(e ast.Expr) {
	switch t := e.(type) {
	case *ast.CompositeLit:
		s.semantifyExpr(t.Type)
		for _, e := range t.Elts {
			s.semantifyExpr(e)
		}
	case *ast.FuncLit:
		savescope, saveblock := s.advanceScopeAndBlock()

		s.processFieldList(t.Type.Params)
		s.processFieldList(t.Type.Results)
		s.processBlockStmt(t.Body)

		s.restoreScopeAndBlock(savescope, saveblock)
	case *ast.Ident:
		if t.Name == "_" {
			return
		}
		d := s.scope.lookup(t.Name);
		if d == nil {
			msg := fmt.Sprintf("Cannot resolve '%s' symbol at %s:%d",
					   t.Name, s.name, t.Line)
			panic(msg)
		}
		s.appendEntry(t.Offset, len(t.Name), t.Line, t.Column, d)
	case *ast.UnaryExpr:
		s.semantifyExpr(t.X)
	case *ast.BinaryExpr:
		s.semantifyExpr(t.X)
		s.semantifyExpr(t.Y)
	case *ast.IndexExpr:
		s.semantifyExpr(t.X)
		s.semantifyExpr(t.Index)
	case *ast.StarExpr:
		s.semantifyExpr(t.X)
	case *ast.CallExpr:
		s.semantifyExpr(t.Fun)
		for _, e := range t.Args {
			s.semantifyExpr(e)
		}
	case *ast.ParenExpr:
		s.semantifyExpr(t.X)
	case *ast.SelectorExpr:
		d := exprToDecl(t.X, s.scope);
		if d == nil {
			msg := fmt.Sprintf("Cannot resolve selector expr at %s:%d (%s)",
					   s.name, t.Pos().Line, t.Sel.Name)
			panic(msg)
		}
		s.semantifyExpr(t.X)
		s.semantifyIdentFor(t.Sel, d)
	case *ast.TypeAssertExpr:
		s.semantifyExpr(t.Type)
		s.semantifyExpr(t.X)
	case *ast.ArrayType:
		s.semantifyExpr(t.Elt)
		s.semantifyExpr(t.Len)
	case *ast.MapType:
		s.semantifyExpr(t.Key)
		s.semantifyExpr(t.Value)
	case *ast.ChanType:
		s.semantifyExpr(t.Value)
	case *ast.FuncType:
		s.semantifyFieldListTypes(t.Params)
		s.semantifyFieldListTypes(t.Results)
	case *ast.SliceExpr:
		s.semantifyExpr(t.X)
		s.semantifyExpr(t.Index)
		s.semantifyExpr(t.End)
	case *ast.KeyValueExpr:
		s.semantifyExpr(t.Key)
		s.semantifyExpr(t.Value)
	default:
		// TODO
	}
}

func (s *SemanticFile) semantifyFieldListFor(fieldList *ast.FieldList, d *Decl) {
	for _, f := range fieldList.List {
		var c *Decl
		for _, name := range f.Names {
			c = s.semantifyIdentFor(name, d)
		}
		if c == nil {
			s.semantifyExpr(f.Type)
		} else {
			s.semantifyTypeFor(f.Type, c)
		}
	}
}

func (s *SemanticFile) semantifyFieldListTypes(fieldList *ast.FieldList) {
	if fieldList == nil {
		return
	}
	for _, f := range fieldList.List {
		s.semantifyExpr(f.Type)
	}
}

func (s *SemanticFile) semantifyFieldList(fieldList *ast.FieldList) {
	for _, f := range fieldList.List {
		for _, name := range f.Names {
			s.semantifyExpr(name)
		}
		s.semantifyExpr(f.Type)
	}
}

func (s *SemanticFile) processDecl(decl ast.Decl) {
	valuescope := s.scope
	foreachDecl(decl, func(data *foreachDeclStruct) {
		valuesSemantified := make(map[ast.Expr]bool, len(data.values))
		class := astDeclClass(data.decl)
		data.tryMakeAnonType(class, 0, s.scope)
		for i, name := range data.names {
			typ, v, vi := data.typeValueIndex(i, 0, s.scope)

			d := NewDecl2(name.Name, class, 0, typ, v, vi, s.scope)
			if d == nil {
				return
			}

			v = data.value(i)
			if _, ok := valuesSemantified[v]; !ok && v != nil {
				valuesSemantified[v] = true
				s.semantifyValueFor(v, d, valuescope)
			}

			if d.Class == DECL_TYPE {
				s.scope = NewScope(s.scope)
				s.addToBlockAndScope(d)
				s.semantifyTypeFor(astDeclType(data.decl), d)
			} else {
				s.semantifyTypeFor(astDeclType(data.decl), d)
				s.scope = NewScope(s.scope)
				s.addToBlockAndScope(d)
			}

			s.semantifyExpr(name)
		}
	})
}

func (s *SemanticFile) processBlockStmt(block *ast.BlockStmt) {
	if block != nil {
		for _, stmt := range block.List {
			s.processStmt(stmt)
		}
	}
}

func (s *SemanticFile) processStmt(stmt ast.Stmt) {
	switch t := stmt.(type) {
	case *ast.DeclStmt:
		s.processDecl(t.Decl)
	case *ast.AssignStmt:
		s.processAssignStmt(t)
	case *ast.IfStmt:
		savescope, saveblock := s.advanceScopeAndBlock()

		s.processStmt(t.Init)
		s.semantifyExpr(t.Cond)
		s.processBlockStmt(t.Body)
		s.processStmt(t.Else)

		s.restoreScopeAndBlock(savescope, saveblock)
	case *ast.BlockStmt:
		savescope, saveblock := s.advanceScopeAndBlock()

		s.processBlockStmt(t)

		s.restoreScopeAndBlock(savescope, saveblock)
	case *ast.RangeStmt:
		s.processRangeStmt(t)
	case *ast.ForStmt:
		savescope, saveblock := s.advanceScopeAndBlock()

		s.processStmt(t.Init)
		s.semantifyExpr(t.Cond)
		s.processStmt(t.Post)
		s.processBlockStmt(t.Body)

		s.restoreScopeAndBlock(savescope, saveblock)
	case *ast.SwitchStmt:
		s.processSwitchStmt(t)
	case *ast.TypeSwitchStmt:
		s.processTypeSwitchStmt(t)
	case *ast.SelectStmt:
		s.processSelectStmt(t)
	case *ast.LabeledStmt:
		s.processStmt(t.Stmt)
	case *ast.ReturnStmt:
		for _, e := range t.Results {
			s.semantifyExpr(e)
		}
	case *ast.ExprStmt:
		s.semantifyExpr(t.X)
	case *ast.IncDecStmt:
		s.semantifyExpr(t.X)
	case *ast.GoStmt:
		s.semantifyExpr(t.Call)
	case *ast.DeferStmt:
		s.semantifyExpr(t.Call)
	}
}

func (s *SemanticFile) processSelectStmt(a *ast.SelectStmt) {
	for _, c := range a.Body.List {
		cc := c.(*ast.CommClause)
		savescope, saveblock := s.advanceScopeAndBlock()

		if cc.Tok == token.DEFINE {
			name := cc.Lhs.(*ast.Ident)
			d := NewDeclVar(name.Name, nil, cc.Rhs, -1, s.scope)
			s.semantifyTypeFor(d.Type, d)
			s.semantifyValueFor(cc.Rhs, d, s.scope)

			s.scope = NewScope(s.scope)
			s.addToBlockAndScope(d)

			s.semantifyExpr(name)
		}

		for _, stmt := range cc.Body {
			s.processStmt(stmt)
		}

		s.restoreScopeAndBlock(savescope, saveblock)
	}
}

func (s *SemanticFile) processSwitchStmt(a *ast.SwitchStmt) {
	savescope, saveblock := s.advanceScopeAndBlock()

	s.processStmt(a.Init)
	s.semantifyExpr(a.Tag)
	for _, c := range a.Body.List {
		cc := c.(*ast.CaseClause)
		for _, v := range cc.Values {
			s.semantifyExpr(v)
		}

		savescope, saveblock := s.advanceScopeAndBlock()

		for _, stmt := range cc.Body {
			s.processStmt(stmt)
		}

		s.restoreScopeAndBlock(savescope, saveblock)
	}

	s.restoreScopeAndBlock(savescope, saveblock)
}

func (s *SemanticFile) processTypeSwitchStmt(a *ast.TypeSwitchStmt) {
	savescope, saveblock := s.advanceScopeAndBlock()

	s.processStmt(a.Init)

	// type variable and its default value
	var tv *Decl
	var def ast.Expr

	switch t := a.Assign.(type) {
	case *ast.AssignStmt:
		lhs := t.Lhs
		rhs := t.Rhs

		ident := lhs[0].(*ast.Ident)
		tv = NewDeclVar(ident.Name, nil, rhs[0], -1, s.scope)
		def = rhs[0]
		s.semantifyExpr(rhs[0])
		s.addToBlockAndScope(tv)
		s.semantifyExpr(lhs[0])
	case *ast.ExprStmt:
		s.semantifyExpr(t.X)
	}

	for _, c := range a.Body.List {
		tcc := c.(*ast.TypeCaseClause)
		// if type variable is available
		if tv != nil {
			if tcc.Types != nil && len(tcc.Types) == 1 {
				// we're in the meaningful type case
				tv.Type = tcc.Types[0]
				tv.Value = nil
			} else {
				// type variable is set to default
				tv.Type = nil
				tv.Value = def
			}
		}

		// semantify types if there are any
		for _, t := range tcc.Types {
			s.semantifyExpr(t)
		}

		savescope, saveblock := s.advanceScopeAndBlock()

		for _, stmt := range tcc.Body {
			s.processStmt(stmt)
		}

		s.restoreScopeAndBlock(savescope, saveblock)
	}

	s.restoreScopeAndBlock(savescope, saveblock)
}

func (s *SemanticFile) processRangeStmt(a *ast.RangeStmt) {
	savescope, saveblock := s.advanceScopeAndBlock()

	s.semantifyExpr(a.X)

	if a.Tok == token.DEFINE {
		if t, ok := a.Key.(*ast.Ident); ok && !s.isInBlock(t.Name) {
			d := NewDeclVar(t.Name, nil, a.X, 0, s.scope)
			if d != nil {
				d.Flags |= DECL_RANGEVAR
				s.addToBlockAndScope(d)
				s.semantifyExpr(t)
			}
		}

		if a.Value != nil {
			if t, ok := a.Value.(*ast.Ident); ok && !s.isInBlock(t.Name) {
				d := NewDeclVar(t.Name, nil, a.X, 1, s.scope)
				if d != nil {
					d.Flags |= DECL_RANGEVAR
					s.addToBlockAndScope(d)
					s.semantifyExpr(t)
				}
			}
		}
	} else {
		s.semantifyExpr(a.Key)
		if a.Value != nil {
			s.semantifyExpr(a.Value)
		}
	}

	s.processBlockStmt(a.Body)

	s.restoreScopeAndBlock(savescope, saveblock)
}

func (s *SemanticFile) processAssignStmt(a *ast.AssignStmt) {
	if a.Tok == token.DEFINE {
		names := make([]*ast.Ident, len(a.Lhs))
		for i, name := range a.Lhs {
			id, ok := name.(*ast.Ident)
			if !ok {
				panic("FAIL")
			}
			names[i] = id
		}

		valuescope := s.scope
		pack := declPack{names, nil, a.Rhs}
		valuesSemantified := make(map[ast.Expr]bool, len(pack.values))
		for i, name := range pack.names {
			if s.isInBlock(name.Name) {
				// it's a shortcut, the variable was already introduced,
				// we only need to semantify it
				s.semantifyExpr(name)
				v, _ := pack.valueIndex(i)
				if _, ok := valuesSemantified[v]; !ok && v != nil {
					valuesSemantified[v] = true
					s.semantifyValueFor(v, s.block.lookup(name.Name), valuescope)
				}
				continue
			}
			typ, v, vi := pack.typeValueIndex(i, 0, s.scope)
			d := NewDeclVar(name.Name, typ, v, vi, s.scope)
			if d == nil {
				continue
			}

			v = pack.value(i)
			if _, ok := valuesSemantified[v]; !ok && v != nil {
				valuesSemantified[v] = true
				s.semantifyValueFor(v, d, valuescope)
			}

			s.scope = NewScope(s.scope)
			s.addToBlockAndScope(d)
			s.semantifyExpr(name)
		}
	} else {
		for _, e := range a.Rhs {
			s.semantifyExpr(e)
		}
		for _, e := range a.Lhs {
			s.semantifyExpr(e)
		}
	}
}

func (s *SemanticFile) advanceScopeAndBlock() (*Scope, *Scope) {
	savescope := s.scope
	saveblock := s.block
	s.scope = NewScope(s.scope)
	s.block = NewScope(nil)

	return savescope, saveblock
}

func (s *SemanticFile) restoreScopeAndBlock(scope *Scope, block *Scope) {
	s.scope = scope
	s.block = block
}

func (s *SemanticFile) isInBlock(name string) bool {
	if s.block == nil {
		panic("FAIL")
	}
	return s.block.lookup(name) != nil
}

func (s *SemanticFile) addToBlockAndScope(d *Decl) {
	s.scope.addNamedDecl(d)
	s.addToBlock(d)
}

func (s *SemanticFile) addToBlock(d *Decl) {
	if s.block != nil {
		s.block.addNamedDecl(d)
	}
}

func (s *SemanticFile) processFieldList(fieldList *ast.FieldList) {
	if fieldList == nil {
		return
	}
	decls := astFieldListToDecls(fieldList, DECL_VAR, 0, s.scope)
	for _, d := range decls {
		s.addToBlockAndScope(d)
	}
	s.semantifyFieldList(fieldList)
}

func (s *SemanticFile) processDeclLocals(decl ast.Decl) {
	switch t := decl.(type) {
	case *ast.FuncDecl:
		methodof := MethodOf(t)
		if methodof != "" {
			d := exprToDecl(ast.NewIdent(methodof), s.scope)
			if d == nil {
				msg := fmt.Sprintf("Can't find a method owner called '%s'",
						   methodof)
				panic(msg)
			}
			s.semantifyIdentFor(t.Name, d)
		} else {
			s.semantifyExpr(t.Name)
		}

		savescope, saveblock := s.advanceScopeAndBlock()

		s.processFieldList(t.Recv)
		s.processFieldList(t.Type.Params)
		s.processFieldList(t.Type.Results)
		s.processBlockStmt(t.Body)

		s.restoreScopeAndBlock(savescope, saveblock)
	case *ast.GenDecl:
		switch t.Tok {
		case token.CONST:
			for _, spec := range t.Specs {
				v := spec.(*ast.ValueSpec)
				for _, name := range v.Names {
					s.semantifyExpr(name)
				}
				for _, value := range v.Values {
					s.semantifyExpr(value)
				}
				if v.Type != nil {
					s.semantifyExpr(v.Type)
				}
			}
		case token.TYPE:
			for _, spec := range t.Specs {
				t := spec.(*ast.TypeSpec)
				s.semantifyExpr(t.Name)
				d := s.scope.lookup(t.Name.Name)
				if d == nil {
					panic("FAIL")
				}
				s.semantifyTypeFor(t.Type, d)
			}
		case token.VAR:
			for _, spec := range t.Specs {
				v := spec.(*ast.ValueSpec)
				decls := make([]*Decl, len(v.Names))
				for i, name := range v.Names {
					s.semantifyExpr(name)
					d := s.scope.lookup(name.Name)
					if d == nil {
						panic(fmt.Sprintf("Can't resolve symbol: '%s' at %s:%d", name.Name, s.name, name.Line))
					}
					decls[i] = d
				}
				if len(v.Values) != 0 {
					if len(decls) > len(v.Values) {
						s.semantifyExpr(v.Values[0])
					} else {
						for i, value := range v.Values {
							s.semantifyValueFor(value, decls[i], s.scope)
						}
					}
				}
				if v.Type != nil {
					s.semantifyTypeFor(v.Type, decls[0])
				}
			}
		}
	}
}

func (s *SemanticFile) semantify() {
	for _, decl := range s.file.Decls {
		s.processDeclLocals(decl)
	}
	s.block = nil
}

//-------------------------------------------------------------------------
// SemanticContext
//-------------------------------------------------------------------------

type SemanticContext struct {
	pkg *Scope

	// TODO: temporary, for testing purposes, should be shared with 
	// AutoCompleteContext.
	declcache *DeclCache
	mcache MCache
}

func NewSemanticContext(mcache MCache, declcache *DeclCache) *SemanticContext {
	return &SemanticContext{
		nil,
		declcache,
		mcache,
	}
}

func (s *SemanticContext) Collect(filename string) []*SemanticFile {
	// 1. Retrieve the file and all other files of the package from the cache.
	// 2. Update modules cache, fixup modules.
	// 3. Merge declarations to the package scope.
	// 4. Infer types for all top-level declarations in each file.
	// 5. Collect semantic information for all identifiers in each file..
	// 6. Sort..
	// 7. Ready to use!

	// NOTE: (5) can be done in parallel for each file

	// 1
	current := s.declcache.Get(filename)
	if current.Error != nil {
		panic("Failed to parse current file")
	}
	others := getOtherPackageFiles(filename, packageName(current.File), s.declcache)
	for _, f := range others {
		if f.Error != nil {
			panic("Failed to parse one of the other files")
		}
	}

	// 2
	ms := make(map[string]*ModuleCache)

	s.mcache.AppendModules(ms, current.Modules)
	for _, f := range others {
		s.mcache.AppendModules(ms, f.Modules)
	}

	updateModules(ms)

	fixupModules(current.FileScope, current.Modules, s.mcache)
	for _, f := range others {
		fixupModules(f.FileScope, f.Modules, s.mcache)
	}

	// 3
	s.pkg = NewScope(universeScope)
	mergeDecls(current.FileScope, s.pkg, current.Decls)
	for _, f := range others {
		mergeDecls(f.FileScope, s.pkg, f.Decls)
	}

	// 4
	for _, d := range s.pkg.entities {
		d.InferType()
	}

	// 5
	files := make([]*SemanticFile, len(others) + 1)
	files[0] = NewSemanticFile(current)
	for i, f := range others {
		files[i+1] = NewSemanticFile(f)
	}

	done := make(chan bool)
	for _, f := range files {
		go func(f *SemanticFile) {
			f.semantify()
			done <- true
		}(f)
	}

	for _ = range files {
		<-done
	}

	return files
}

//-------------------------------------------------------------------------
// SemanticContext.GetSMap
// Returns a list of entities for the 'filename' which have semantic
// information. Useful for testing only.
//-------------------------------------------------------------------------

type DeclDesc struct {
	//Id int
	Offset int
	Length int
}

func (s *SemanticContext) GetSMap(filename string) []DeclDesc {
	files := s.Collect(filename)

	i := 0
	f := files[0]
	decls := make([]DeclDesc, len(f.entries))
	for _, e := range f.entries {
		// append
		decls[i].Length = e.length
		decls[i].Offset = e.offset
		i++
	}

	return decls
}

//-------------------------------------------------------------------------
// SemanticContext.Rename
//-------------------------------------------------------------------------

// TODO: length is always the same for a set of identifiers
type RenameDeclDesc struct {
	Offset int
	Line int
	Col int
}

type RenameDesc struct {
	Length int
	Filename string
	Decls []RenameDeclDesc
}

func (d *RenameDesc) append(offset, line, col int) {
	if d.Decls == nil {
		d.Decls = make([]RenameDeclDesc, 0, 16)
	}

	n := len(d.Decls)
	if cap(d.Decls) < n+1 {
		s := make([]RenameDeclDesc, n, n*2+1)
		copy(s, d.Decls)
		d.Decls = s
	}

	d.Decls = d.Decls[0 : n+1]
	d.Decls[n] = RenameDeclDesc{offset, line, col}
}

func (s *SemanticContext) Rename(filename string, cursor int) []RenameDesc {
	files := s.Collect(filename)

	var theDecl *Decl
	// find the declaration under the cursor
	for _, e := range files[0].entries {
		if cursor >= e.offset && cursor < e.offset + e.length {
			theDecl = e.decl
			break
		}
	}

	// if there is no such declaration, return nil
	if theDecl == nil {
		return nil
	}

	// foreign declarations and universe scope declarations are a no-no too
	if theDecl.Flags&DECL_FOREIGN != 0 || theDecl.Scope == universeScope {
		return nil
	}

	// collect decldescs about this declaration in each file
	renames := make([]RenameDesc, len(files))
	for i, f := range files {
		renames[i].Filename = f.name
		for _, e := range f.entries {
			if e.decl == theDecl {
				renames[i].append(e.offset, e.line, e.col)
				renames[i].Length = e.length
			}
		}
	}

	return renames
}
