package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

//-------------------------------------------------------------------------
// package_import
//
// Contains import information from a single file
//-------------------------------------------------------------------------

type package_import struct {
	alias string
	path  string
}

type package_imports []package_import

func new_package_imports(filename string, decls []ast.Decl) package_imports {
	mi := make(package_imports, 0, 16)
	mi.append_imports(filename, decls)
	return mi
}

// Parses import declarations until the first non-import declaration and fills
// 'pi' array with import information.
func (pi *package_imports) append_imports(filename string, decls []ast.Decl) {
	for _, decl := range decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			for _, spec := range gd.Specs {
				imp := spec.(*ast.ImportSpec)
				path, alias := path_and_alias(imp)
				path, ok := abs_path_for_package(filename, path)
				if ok {
					pi.append_import(alias, path)
				}
			}
		} else {
			return
		}
	}
}

// Simple vector-like append.
func (pi *package_imports) append_import(alias, path string) {
	if alias == "_" {
		return
	}

	*pi = append(*pi, package_import{alias, path})
}

//-------------------------------------------------------------------------
// decl_file_cache
//
// Contains cache for top-level declarations of a file as well as its
// contents, AST and import information. Used in both autocompletion
// and refactoring utilities.
//-------------------------------------------------------------------------

type decl_file_cache struct {
	name  string // file name
	mtime int64  // last modification time

	decls     map[string]*decl // top-level declarations
	error     error            // last error
	packages  package_imports  // import information
	filescope *scope

	fset *token.FileSet
}

func new_decl_file_cache(name string) *decl_file_cache {
	f := new(decl_file_cache)
	f.name = name
	return f
}

func (f *decl_file_cache) update() {
	stat, err := os.Stat(f.name)
	if err != nil {
		f.decls = nil
		f.error = err
		f.fset = nil
		return
	}

	statmtime := stat.ModTime().UnixNano()
	if f.mtime == statmtime {
		return
	}

	f.mtime = statmtime
	f.read_file()
}

func (f *decl_file_cache) read_file() {
	var data []byte
	data, f.error = file_reader.read_file(f.name)
	if f.error != nil {
		return
	}
	data, _ = filter_out_shebang(data)

	f.process_data(data)
}

func (f *decl_file_cache) process_data(data []byte) {
	var file *ast.File
	f.fset = token.NewFileSet()
	file, f.error = parser.ParseFile(f.fset, "", data, 0)
	f.filescope = new_scope(nil)
	for _, d := range file.Decls {
		anonymify_ast(d, 0, f.filescope)
	}
	f.packages = new_package_imports(f.name, file.Decls)
	f.decls = make(map[string]*decl, len(file.Decls))
	for _, decl := range file.Decls {
		append_to_top_decls(f.decls, decl, f.filescope)
	}
}

func append_to_top_decls(decls map[string]*decl, decl ast.Decl, scope *scope) {
	foreach_decl(decl, func(data *foreach_decl_struct) {
		class := ast_decl_class(data.decl)
		for i, name := range data.names {
			typ, v, vi := data.type_value_index(i)

			d := new_decl_full(name.Name, class, 0, typ, v, vi, scope)
			if d == nil {
				return
			}

			methodof := method_of(decl)
			if methodof != "" {
				decl, ok := decls[methodof]
				if ok {
					decl.add_child(d)
				} else {
					decl = new_decl(methodof, decl_methods_stub, scope)
					decls[methodof] = decl
					decl.add_child(d)
				}
			} else {
				decl, ok := decls[d.name]
				if ok {
					decl.expand_or_replace(d)
				} else {
					decls[d.name] = d
				}
			}
		}
	})
}

func abs_path_for_package(filename, p string) (string, bool) {
	dir, _ := filepath.Split(filename)
	if len(p) == 0 {
		return "", false
	}
	if p[0] == '.' {
		return fmt.Sprintf("%s.a", filepath.Join(dir, p)), true
	}
	pkg, ok := find_go_dag_package(p, dir)
	if ok {
		return pkg, true
	}
	return find_global_file(p)
}

func path_and_alias(imp *ast.ImportSpec) (string, string) {
	path := ""
	if imp.Path != nil {
		path = string(imp.Path.Value)
		path = path[1 : len(path)-1]
	}
	alias := ""
	if imp.Name != nil {
		alias = imp.Name.Name
	}
	return path, alias
}

func find_go_dag_package(imp, filedir string) (string, bool) {
	// Support godag directory structure
	dir, pkg := filepath.Split(imp)
	godag_pkg := filepath.Join(filedir, "..", dir, "_obj", pkg+".a")
	if file_exists(godag_pkg) {
		return godag_pkg, true
	}
	return "", false
}

func find_global_file(imp string) (string, bool) {
	// gocode synthetically generates the builtin package
	// "unsafe", since the "unsafe.a" package doesn't really exist.
	// Thus, when the user request for the package "unsafe" we
	// would return synthetic global file that would be used
	// just as a key name to find this synthetic package
	if imp == "unsafe" {
		return "unsafe", true
	}

	pkgfile := fmt.Sprintf("%s.a", imp)

	// if lib-path is defined, use it
	if g_config.LibPath != "" {
		for _, p := range filepath.SplitList(g_config.LibPath) {
			pkg_path := filepath.Join(p, pkgfile)
			if file_exists(pkg_path) {
				return pkg_path, true
			}
		}
	}

	// otherwise figure out the default lib-path
	gopath := os.Getenv("GOPATH")
	goroot := os.Getenv("GOROOT")
	goarch := os.Getenv("GOARCH")
	goos := os.Getenv("GOOS")
	if goroot == "" {
		goroot = runtime.GOROOT()
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	if goos == "" {
		goos = runtime.GOOS
	}

	pkgdir := fmt.Sprintf("%s_%s", goos, goarch)
	pkgpath := filepath.Join("pkg", pkgdir, pkgfile)

	if gopath != "" {
		for _, p := range filepath.SplitList(gopath) {
			gopath_pkg := filepath.Join(p, pkgpath)
			if file_exists(gopath_pkg) {
				return gopath_pkg, true
			}
		}
	}
	goroot_pkg := filepath.Join(goroot, pkgpath)
	return goroot_pkg, file_exists(goroot_pkg)
}

func package_name(file *ast.File) string {
	if file.Name != nil {
		return file.Name.Name
	}
	return ""
}

//-------------------------------------------------------------------------
// decl_cache
//
// Thread-safe collection of DeclFileCache entities.
//-------------------------------------------------------------------------

type decl_cache struct {
	cache map[string]*decl_file_cache
	sync.Mutex
}

func new_decl_cache() *decl_cache {
	c := new(decl_cache)
	c.cache = make(map[string]*decl_file_cache)
	return c
}

func (c *decl_cache) get(filename string) *decl_file_cache {
	c.Lock()
	defer c.Unlock()

	f, ok := c.cache[filename]
	if !ok {
		f = new_decl_file_cache(filename)
		c.cache[filename] = f
	}
	return f
}

func (c *decl_cache) get_and_update(filename string) *decl_file_cache {
	f := c.get(filename)
	f.update()
	return f
}
