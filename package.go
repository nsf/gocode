package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log"
	"strings"

	"github.com/visualfc/gocode/internal/gcexportdata"
	"golang.org/x/tools/go/types/typeutil"
)

type package_parser interface {
	parse_export(callback func(pkg string, decl ast.Decl))
}

//-------------------------------------------------------------------------
// package_file_cache
//
// Structure that represents a cache for an imported pacakge. In other words
// these are the contents of an archive (*.a) file.
//-------------------------------------------------------------------------

type package_file_cache struct {
	name        string // file name
	import_name string
	vendor_name string
	mtime       int64
	defalias    string

	scope  *scope
	main   *decl // package declaration
	others map[string]*decl
}

func new_package_file_cache(absname, name string, vname string) *package_file_cache {
	m := new(package_file_cache)
	m.name = absname
	m.import_name = name
	m.vendor_name = vname
	m.mtime = 0
	m.defalias = ""
	return m
}

// Creates a cache that stays in cache forever. Useful for built-in packages.
func new_package_file_cache_forever(name, defalias string) *package_file_cache {
	m := new(package_file_cache)
	m.name = name
	m.mtime = -1
	m.defalias = defalias
	return m
}

func (m *package_file_cache) find_file() string {
	if file_exists(m.name) {
		return m.name
	}

	n := len(m.name)
	filename := m.name[:n-1] + "6"
	if file_exists(filename) {
		return filename
	}

	filename = m.name[:n-1] + "8"
	if file_exists(filename) {
		return filename
	}

	filename = m.name[:n-1] + "5"
	if file_exists(filename) {
		return filename
	}
	return m.name
}

func (m *package_file_cache) update_cache(c *auto_complete_context) {
	if m.mtime == -1 {
		return
	}
	// defer func() {
	// 	if err := recover(); err != nil {
	// 		log.Println("update_cache recover error:", err)
	// 	}
	// }()

	import_path := m.import_name
	if m.vendor_name != "" {
		import_path = m.vendor_name
	}
	if pkg := c.typesWalker.Imported[import_path]; pkg != nil {
		if pkg.Name() == "" {
			log.Println("error parser", import_path)
			return
		}
		if chk, ok := c.typesWalker.ImportedFilesCheck[import_path]; ok {
			if m.mtime == chk.ModTime {
				return
			}
			m.mtime = chk.ModTime
		}
		m.process_package_types(c, pkg)
		return
	}
	m.process_package_data(c, nil, true)

	// fname := m.find_file()
	// stat, err := os.Stat(fname)

	// if err != nil {
	// 	m.process_package_data(c, nil, true)
	// 	return
	// }
	// statmtime := stat.ModTime().UnixNano()
	// if m.mtime != statmtime {
	// 	m.mtime = statmtime
	// 	data, err := file_reader.read_file(fname)
	// 	if err != nil {
	// 		return
	// 	}
	// 	m.process_package_data(c, data, false)
	// }
}

type types_export struct {
	pkg *types.Package
	pfc *package_file_cache
}

func (p *types_export) init(pkg *types.Package, pfc *package_file_cache) {
	p.pkg = pkg
	p.pfc = pfc
	pfc.defalias = pkg.Name()
	for _, pkg := range p.pkg.Imports() {
		pkgid := "!" + pkg.Path() + "!" + pkg.Name()
		pfc.add_package_to_scope(pkgid, pkg.Path())
	}
	pkgid := "!" + pkg.Path() + "!" + pkg.Name()
	pfc.add_package_to_scope(pkgid, pkg.Path())
}

func (p *types_export) parse_export(callback func(pkg string, decl ast.Decl)) {
	for _, pkg := range p.pkg.Imports() {
		pkg_parse_export(pkg, p.pfc, callback)
	}
	pkg_parse_export(p.pkg, p.pfc, callback)
}

func pkg_parse_export(pkg *types.Package, pfc *package_file_cache, callback func(pkg string, decl ast.Decl)) {
	pkgid := "!" + pkg.Path() + "!" + pkg.Name()
	//pfc.add_package_to_scope(pkgid, pkg.Path())

	for _, name := range pkg.Scope().Names() {
		if obj := pkg.Scope().Lookup(name); obj != nil {
			if !obj.Exported() {
				continue
			}
			name := obj.Name()
			var decl ast.Decl
			switch t := obj.(type) {
			case *types.Const:
				decl = &ast.GenDecl{
					Tok: token.CONST,
					Specs: []ast.Spec{
						&ast.ValueSpec{
							Names: []*ast.Ident{ast.NewIdent(name)},
							Type:  toType(pkg, t.Type()),
						},
					},
				}
			case *types.Var:
				decl = &ast.GenDecl{
					Tok: token.VAR,
					Specs: []ast.Spec{
						&ast.ValueSpec{
							Names: []*ast.Ident{ast.NewIdent(name)},
							Type:  toType(pkg, t.Type()),
						},
					},
				}
			case *types.TypeName:
				decl = &ast.GenDecl{
					Tok:   token.TYPE,
					Specs: []ast.Spec{toTypeSpec(pkg, t)},
				}
				if named, ok := t.Type().(*types.Named); ok {
					for _, sel := range typeutil.IntuitiveMethodSet(named, nil) {
						sig := sel.Type().(*types.Signature)
						decl := &ast.FuncDecl{
							Recv: toRecv(pkg, sig.Recv()),
							Name: ast.NewIdent(sel.Obj().Name()),
							Type: toFuncType(pkg, sig),
						}
						callback(pkgid, decl)
					}
				}
			case *types.Func:
				sig := t.Type().(*types.Signature)
				decl = &ast.FuncDecl{
					Recv: toRecv(pkg, sig.Recv()),
					Name: ast.NewIdent(name),
					Type: toFuncType(pkg, sig),
				}
			}
			callback(pkgid, decl)
		}
	}
}

func (m *package_file_cache) process_package_types(c *auto_complete_context, pkg *types.Package) {
	m.scope = new_named_scope(g_universe_scope, m.name)

	// main package
	m.main = new_decl(m.name, decl_package, nil)
	// create map for other packages
	m.others = make(map[string]*decl)

	var pp package_parser
	fset := token.NewFileSet()
	var buf bytes.Buffer
	gcexportdata.Write(&buf, fset, pkg)
	var p gc_bin_parser
	p.init(buf.Bytes(), m)
	pp = &p

	prefix := "!" + m.name + "!"
	//log.Println("load pkg", pkg.Path(), pkg.Imports())
	pp.parse_export(func(pkg string, decl ast.Decl) {
		anonymify_ast(decl, decl_foreign, m.scope)
		if pkg == "" || strings.HasPrefix(pkg, prefix) {
			// main package
			add_ast_decl_to_package(m.main, decl, m.scope)
		} else {
			// others
			if _, ok := m.others[pkg]; !ok {
				m.others[pkg] = new_decl(pkg, decl_package, nil)
			}
			add_ast_decl_to_package(m.others[pkg], decl, m.scope)
		}
	})

	// hack, add ourselves to the package scope
	mainName := "!" + m.name + "!" + m.defalias
	m.add_package_to_scope(mainName, m.name)

	// replace dummy package decls in package scope to actual packages
	for key := range m.scope.entities {
		if !strings.HasPrefix(key, "!") {
			continue
		}
		pkg, ok := m.others[key]
		if !ok && key == mainName {
			pkg = m.main
		}
		m.scope.replace_decl(key, pkg)
	}
}

func (m *package_file_cache) process_package_data(c *auto_complete_context, data []byte, source bool) {
	m.scope = new_named_scope(g_universe_scope, m.name)

	// main package
	m.main = new_decl(m.name, decl_package, nil)
	// create map for other packages
	m.others = make(map[string]*decl)

	var pp package_parser
	if source {
		var tp types_parser
		var srcDir string
		importPath := m.import_name
		if m.vendor_name != "" {
			importPath = m.vendor_name
		}
		tp.initSource(m.import_name, importPath, srcDir, m, c)
		if tp.pkg != nil && tp.pkg.Name() == "" {
			log.Println("error parser data source", importPath)
			return
		}
		data = tp.exportData()
		if *g_debug {
			log.Printf("parser source %q %q\n", importPath, srcDir)
		}
		if data == nil {
			log.Println("error parser data source", importPath)
			return
		}
		var p gc_bin_parser
		p.init(data, m)
		pp = &p
	} else {
		i := bytes.Index(data, []byte{'\n', '$', '$'})
		if i == -1 {
			panic(fmt.Sprintf("Can't find the import section in the package file %s", m.name))
		}
		offset := i + len("\n$$")
		if data[offset] == 'B' {
			// binary format, skip 'B\n'
			//data = data[2:]
			if data[offset+2] == 'i' {
				var tp types_parser
				tp.initData(m.import_name, data, m, c)
				data = tp.exportData()
				if data == nil {
					log.Println("error parser data binary", m.import_name)
					return
				}
			} else {
				data = data[offset+2:]
			}
			var p gc_bin_parser
			p.init(data, m)
			pp = &p
		} else {
			data = data[offset:]
			// textual format, find the beginning of the package clause
			i := bytes.Index(data, []byte{'p', 'a', 'c', 'k', 'a', 'g', 'e'})
			if i == -1 {
				panic("Can't find the package clause")
			}
			data = data[i:]

			var p gc_parser
			p.init(data, m)
			pp = &p
		}
	}

	prefix := "!" + m.name + "!"
	pp.parse_export(func(pkg string, decl ast.Decl) {
		anonymify_ast(decl, decl_foreign, m.scope)
		if pkg == "" || strings.HasPrefix(pkg, prefix) {
			// main package
			add_ast_decl_to_package(m.main, decl, m.scope)
		} else {
			// others
			if _, ok := m.others[pkg]; !ok {
				m.others[pkg] = new_decl(pkg, decl_package, nil)
			}
			add_ast_decl_to_package(m.others[pkg], decl, m.scope)
		}
	})

	// hack, add ourselves to the package scope
	mainName := "!" + m.name + "!" + m.defalias
	m.add_package_to_scope(mainName, m.name)

	// replace dummy package decls in package scope to actual packages
	for key := range m.scope.entities {
		if !strings.HasPrefix(key, "!") {
			continue
		}
		pkg, ok := m.others[key]
		if !ok && key == mainName {
			pkg = m.main
		}
		m.scope.replace_decl(key, pkg)
	}
}

func (m *package_file_cache) add_package_to_scope(alias, realname string) {
	d := new_decl(realname, decl_package, nil)
	m.scope.add_decl(alias, d)
}

func add_ast_decl_to_package(pkg *decl, decl ast.Decl, scope *scope) {
	foreach_decl(decl, func(data *foreach_decl_struct) {
		class := ast_decl_class(data.decl)
		typeparams := ast_decl_typeparams(data.decl)
		for i, name := range data.names {
			typ, v, vi := data.type_value_index(i)

			d := new_decl_full(name.Name, class, decl_foreign|ast_decl_flags(data.decl), typ, v, vi, scope)
			if d == nil {
				continue
			}
			d.typeparams = typeparams

			if !name.IsExported() && d.class != decl_type {
				return
			}

			methodof := method_of(data.decl)
			if methodof != "" {
				decl := pkg.find_child(methodof)
				if decl != nil {
					decl.add_child(d)
				} else {
					decl = new_decl(methodof, decl_methods_stub, scope)
					decl.add_child(d)
					pkg.add_child(decl)
				}
			} else {
				decl := pkg.find_child(d.name)
				if decl != nil {
					decl.expand_or_replace(d)
				} else {
					pkg.add_child(d)
				}
			}
		}
	})
}

//-------------------------------------------------------------------------
// package_cache
//-------------------------------------------------------------------------

type package_cache map[string]*package_file_cache

func new_package_cache() package_cache {
	m := make(package_cache)

	// add built-in "unsafe" package
	m.add_builtin_unsafe_package()

	return m
}

// Function fills 'ps' set with packages from 'packages' import information.
// In case if package is not in the cache, it creates one and adds one to the cache.
func (c package_cache) append_packages(ps map[string]*package_file_cache, pkgs []package_import) {
	for _, m := range pkgs {
		if _, ok := ps[m.abspath]; ok {
			continue
		}

		if mod, ok := c[m.abspath]; ok {
			ps[m.abspath] = mod
		} else {
			mod = new_package_file_cache(m.abspath, m.path, m.vpath)
			ps[m.abspath] = mod
			c[m.abspath] = mod
		}
	}
}

func (c package_cache) add_builtin_unsafe_package() {
	pkg := new_package_file_cache_forever("unsafe", "unsafe")
	pkg.process_package_data(nil, g_builtin_unsafe_package, false)
	c["unsafe"] = pkg
}
