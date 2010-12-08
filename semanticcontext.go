package main

import (
	"go/ast"
	"go/token"
	"strings"
	"sort"
	"fmt"
	"os"
)

type SemanticEntry struct {
	offset int
	length int
	line   int
	col    int
	decl   *Decl
}

//-------------------------------------------------------------------------
// SemanticFile
//-------------------------------------------------------------------------

type SemanticFile struct {
	name      string
	data      []byte
	entries   []SemanticEntry
	file      *ast.File
	filescope *Scope

	scope *Scope
	block *Scope // that one is used temporary for 'redeclarations' case

	fset *token.FileSet
}

func NewSemanticFile(dc *DeclFileCache) *SemanticFile {
	s := new(SemanticFile)
	s.name = dc.name
	s.data = dc.Data
	s.entries = make([]SemanticEntry, 0, 16)
	s.file = dc.File
	s.filescope = dc.FileScope
	s.scope = s.filescope
	s.fset = dc.fset
	return s
}

func (s *SemanticFile) semantifyIdentFor(e *ast.Ident, d *Decl) {
	c := d.FindChildAndInEmbedded(e.Name)
	if c == nil {
		msg := fmt.Sprintf("Cannot resolve '%s' symbol at %s:%d",
			e.Name, s.name, s.fset.Position(e.NamePos).Line)
		panic(msg)
	}
	s.entries = append(s.entries, SemanticEntry{
		s.fset.Position(e.NamePos).Offset,
		len(e.Name),
		s.fset.Position(e.NamePos).Line,
		s.fset.Position(e.NamePos).Column,
		c,
	})
}

// used for 'type <blabla> struct | interface' declarations
// and of course for anonymous types
func (s *SemanticFile) semantifyTypeFor(e ast.Expr, d *Decl) {
	switch t := e.(type) {
	case *ast.StructType:
		s.semantifyFieldListFor(t.Fields, d)
	case *ast.InterfaceType:
		s.semantifyFieldListFor(t.Methods, d)
	default:
		s.semantifyExpr(e)
	}
}

func (s *SemanticFile) semantifyIdent(t *ast.Ident) *Decl {
	if t.Name == "_" {
		return nil
	}
	d := s.scope.lookup(t.Name)
	if d == nil {
		msg := fmt.Sprintf("Cannot resolve '%s' symbol at %s:%d",
			t.Name, s.name, s.fset.Position(t.NamePos).Line)
		panic(msg)
	}
	if strings.HasPrefix(d.Name, "$") {
		// anonymous type
		s.semantifyTypeFor(d.Type, d)
	}
	s.entries = append(s.entries, SemanticEntry{
		s.fset.Position(t.NamePos).Offset,
		len(t.Name),
		s.fset.Position(t.NamePos).Line,
		s.fset.Position(t.NamePos).Column,
		d,
	})
	return d
}

func (s *SemanticFile) semantifyCompositeLit(c *ast.CompositeLit) {
	s.semantifyExpr(c.Type)
	d := exprToDecl(c.Type, s.scope)
	for _, e := range c.Elts {
		if d != nil && len(d.Children) != 0 {
			switch t := e.(type) {
			case *ast.KeyValueExpr:
				if ident, ok := t.Key.(*ast.Ident); ok {
					s.semantifyIdentFor(ident, d)
				} else {
					s.semantifyExpr(t.Key)
				}
				s.semantifyExpr(t.Value)
			default:
				s.semantifyExpr(e)
			}
		} else {
			s.semantifyExpr(e)
		}
	}
}

func (s *SemanticFile) semantifyExpr(e ast.Expr) {
	switch t := e.(type) {
	case *ast.CompositeLit:
		s.semantifyCompositeLit(t)
	case *ast.FuncLit:
		s.semantifyFieldListTypes(t.Type.Params)
		s.semantifyFieldListTypes(t.Type.Results)

		savescope, saveblock := s.advanceScopeAndBlock()

		s.processFieldList(t.Type.Params, savescope)
		s.processFieldList(t.Type.Results, savescope)
		s.processBlockStmt(t.Body)

		s.restoreScopeAndBlock(savescope, saveblock)
	case *ast.Ident:
		s.semantifyIdent(t)
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
		d := exprToDecl(t.X, s.scope)
		if d == nil {
			msg := fmt.Sprintf("Cannot resolve selector expr at %s:%d (%s)",
				s.name, s.fset.Position(t.Pos()).Line, t.Sel.Name)
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
	}
}

func (s *SemanticFile) semantifyFieldListFor(fieldList *ast.FieldList, d *Decl) {
	for _, f := range fieldList.List {
		for _, name := range f.Names {
			s.semantifyIdentFor(name, d)
		}
		s.semantifyExpr(f.Type)
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

func (s *SemanticFile) semantifyFieldListNames(fieldList *ast.FieldList) {
	if fieldList == nil {
		return
	}
	for _, f := range fieldList.List {
		for _, name := range f.Names {
			s.semantifyIdent(name)
		}
	}
}

func (s *SemanticFile) semantifyFieldList(fieldList *ast.FieldList) {
	for _, f := range fieldList.List {
		for _, name := range f.Names {
			s.semantifyIdent(name)
		}
		s.semantifyExpr(f.Type)
	}
}

func (s *SemanticFile) processDecl(decl ast.Decl) {
	foreachDecl(decl, func(data *foreachDeclStruct) {
		class := astDeclClass(data.decl)
		if class != DECL_TYPE {
			s.semantifyExpr(data.typ)
		}
		for _, v := range data.values {
			s.semantifyExpr(v)
		}
		for i, name := range data.names {
			typ, v, vi := data.typeValueIndex(i, 0)

			if class == DECL_TYPE {
				s.scope = NewScope(s.scope)
				d := NewDecl2(name.Name, class, 0, typ, v, vi, s.scope)
				if d == nil {
					return
				}
				s.addToBlockAndScope(d)
				s.semantifyTypeFor(astDeclType(data.decl), d)
			} else {
				d := NewDecl2(name.Name, class, 0, typ, v, vi, s.scope)
				if d == nil {
					return
				}
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
			s.semantifyExpr(cc.Rhs)

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
		if t, ok := a.Key.(*ast.Ident); ok {
			d := NewDeclVar(t.Name, nil, a.X, 0, s.scope)
			if d != nil {
				d.Flags |= DECL_RANGEVAR
				s.scope = NewScope(s.scope)
				s.addToBlockAndScope(d)
				s.semantifyExpr(t)
			}
		}

		if a.Value != nil {
			if t, ok := a.Value.(*ast.Ident); ok {
				d := NewDeclVar(t.Name, nil, a.X, 1, s.scope)
				if d != nil {
					d.Flags |= DECL_RANGEVAR
					s.scope = NewScope(s.scope)
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

		pack := declPack{names, nil, a.Rhs}
		for _, v := range pack.values {
			s.semantifyExpr(v)
		}
		for i, name := range pack.names {
			if s.isInBlock(name.Name) {
				// it's a shortcut, the variable was already introduced,
				// we only need to semantify it
				s.semantifyExpr(name)
				continue
			}
			typ, v, vi := pack.typeValueIndex(i, 0)
			d := NewDeclVar(name.Name, typ, v, vi, s.scope)
			if d == nil {
				continue
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

func (s *SemanticFile) processFieldList(fieldList *ast.FieldList, scope *Scope) {
	if fieldList == nil {
		return
	}
	decls := astFieldListToDecls(fieldList, DECL_VAR, 0, scope)
	for _, d := range decls {
		s.addToBlockAndScope(d)
	}
	s.semantifyFieldListNames(fieldList)
}

func (s *SemanticFile) processTopDecls(decl ast.Decl) {
	switch t := decl.(type) {
	case *ast.FuncDecl:
		methodof := MethodOf(t)
		if methodof != "" {
			d := s.scope.lookup(methodof)
			if d == nil {
				msg := fmt.Sprintf("Can't find a method owner called '%s'",
					methodof)
				panic(msg)
			}
			s.semantifyIdentFor(t.Name, d)
		} else {
			s.semantifyExpr(t.Name)
		}

		s.semantifyFieldListTypes(t.Recv)
		s.semantifyFieldListTypes(t.Type.Params)
		s.semantifyFieldListTypes(t.Type.Results)

		savescope, saveblock := s.advanceScopeAndBlock()

		s.processFieldList(t.Recv, savescope)
		s.processFieldList(t.Type.Params, savescope)
		s.processFieldList(t.Type.Results, savescope)
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
						panic(fmt.Sprintf("Can't resolve symbol: '%s' at %s:%d", name.Name, s.name, s.fset.Position(name.NamePos).Line))
					}
					decls[i] = d
				}
				if len(v.Values) != 0 {
					for _, v := range v.Values {
						s.semantifyExpr(v)
					}
				}
				if v.Type != nil {
					s.semantifyExpr(v.Type)
				}
			}
		}
	}
}

func (s *SemanticFile) semantify() {
	for _, decl := range s.file.Decls {
		s.processTopDecls(decl)
	}
	s.block = nil
}

//-------------------------------------------------------------------------
// SemanticContext
//-------------------------------------------------------------------------

type SemanticContext struct {
	pkg       *Scope
	declcache *DeclCache
	pcache    PackageCache
}

func NewSemanticContext(pcache PackageCache, declcache *DeclCache) *SemanticContext {
	return &SemanticContext{
		nil,
		declcache,
		pcache,
	}
}

func (s *SemanticContext) Collect(filename string) ([]*SemanticFile, os.Error) {
	// 1. Retrieve the file and all other files of the package from the cache.
	// 2. Update packages cache, fixup packages.
	// 3. Merge declarations to the package scope.
	// 4. Infer types for all top-level declarations in each file.
	// 5. Collect semantic information for all identifiers in each file..
	// 6. Sort..
	// 7. Ready to use!

	// NOTE: (5) can be done in parallel for each file

	// 1
	const errhead = "Unable to proceed, because of the following errors:\n"

	current := s.declcache.Get(filename)
	if current.Error != nil {
		err := fmt.Sprintf("%sFailed to parse '%s'", errhead, filename)
		return nil, os.NewError(err)
	}
	others := getOtherPackageFiles(filename, packageName(current.File), s.declcache)
	for _, f := range others {
		if f.Error != nil {
			err := fmt.Sprintf("%sFailed to parse '%s'", errhead, f.name)
			return nil, os.NewError(err)
		}
	}

	// 2
	ps := make(map[string]*PackageFileCache)

	s.pcache.AppendPackages(ps, current.Packages)
	for _, f := range others {
		s.pcache.AppendPackages(ps, f.Packages)
	}

	updatePackages(ps)

	fixupPackages(current.FileScope, current.Packages, s.pcache)
	for _, f := range others {
		fixupPackages(f.FileScope, f.Packages, s.pcache)
	}

	// 3
	s.pkg = NewScope(universeScope)
	mergeDecls(current.FileScope, s.pkg, current.Decls)
	mergeDeclsFromPackages(s.pkg, current.Packages, s.pcache)
	for _, f := range others {
		mergeDecls(f.FileScope, s.pkg, f.Decls)
		mergeDeclsFromPackages(s.pkg, f.Packages, s.pcache)
	}

	// 4
	for _, d := range s.pkg.entities {
		d.InferType()
	}

	// 5
	files := make([]*SemanticFile, len(others)+1)
	files[0] = NewSemanticFile(current)
	for i, f := range others {
		files[i+1] = NewSemanticFile(f)
	}

	done := make(chan os.Error)
	for _, f := range files {
		go func(f *SemanticFile) {
			defer func() {
				if err := recover(); err != nil {
					printBacktrace(err)
					serr, ok := err.(string)
					if !ok {
						serr = "Unknown error"
					}
					done <- os.NewError(serr)
				}
			}()

			f.semantify()
			done <- nil
		}(f)
	}

	errs := ""
	for _ = range files {
		err := <-done
		if err != nil {
			errs += err.String() + "\n"
		}
	}

	if errs == "" {
		return files, nil
	}
	errs = "Unable to proceed, because of the following errors:\n" + errs[:len(errs)-1]
	return nil, os.NewError(errs)
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
	files, err := s.Collect(filename)
	if err != nil {
		panic(err.String())
	}

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

type RenameDeclDesc struct {
	Offset int
	Line   int
	Col    int
}

type RenameDesc struct {
	Length   int
	Filename string
	Decls    []RenameDeclDesc
}

// sort in a reverse order
func (d *RenameDesc) Len() int           { return len(d.Decls) }
func (d *RenameDesc) Less(i, j int) bool { return !(d.Decls[i].Offset < d.Decls[j].Offset) }
func (d *RenameDesc) Swap(i, j int)      { d.Decls[i], d.Decls[j] = d.Decls[j], d.Decls[i] }

func (d *RenameDesc) append(offset, line, col int) {
	d.Decls = append(d.Decls, RenameDeclDesc{offset, line, col})
}

func (s *SemanticContext) Rename(filename string, cursor int) ([]RenameDesc, os.Error) {
	files, err := s.Collect(filename)
	if err != nil {
		return nil, err
	}

	var theDecl *Decl
	// find the declaration under the cursor
	for _, e := range files[0].entries {
		if cursor >= e.offset && cursor < e.offset+e.length {
			theDecl = e.decl
			break
		}
	}

	// if there is no such declaration, return nil
	if theDecl == nil {
		return nil, os.NewError("Failed to find valid declaration under the cursor")
	}

	// foreign declarations and universe scope declarations are a no-no too
	if theDecl.Flags&DECL_FOREIGN != 0 || theDecl.Scope == universeScope {
		return nil, os.NewError("Can't rename foreign or built-in declaration")
	}

	// I need that for testing currently, and maybe some people will want that
	// too.
	if Config.DenyPackageRenames && theDecl.Class == DECL_PACKAGE {
		return nil, os.NewError("Package rename operation is denied by the configuration")
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
		sort.Sort(&renames[i])
	}

	return renames, nil
}
