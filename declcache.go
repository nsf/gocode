package main

import (
	"os"
	"go/ast"
	"go/parser"
	"io/ioutil"
	"go/token"
	"runtime"
	"path"
	"fmt"
	"sync"
)

//-------------------------------------------------------------------------
// PackageImport
//
// Contains import information from a single file
//-------------------------------------------------------------------------

type PackageImport struct {
	Alias string
	Path  string
}

type PackageImports []PackageImport

func NewPackageImports(filename string, decls []ast.Decl) PackageImports {
	mi := make(PackageImports, 0, 16)
	mi.appendImports(filename, decls)
	return mi
}

// Parses import declarations until the first non-import declaration and fills
// 'pi' array with import information.
func (pi *PackageImports) appendImports(filename string, decls []ast.Decl) {
	for _, decl := range decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			for _, spec := range gd.Specs {
				imp := spec.(*ast.ImportSpec)
				path, alias := pathAndAlias(imp)
				path = absPathForPackage(filename, path)
				pi.appendImport(alias, path)
			}
		} else {
			return
		}
	}
}

// Simple vector-like append.
func (pi *PackageImports) appendImport(alias, path string) {
	v := *pi
	if alias == "_" || alias == "." {
		// TODO: support for packages imported in the current namespace
		return
	}

	n := len(v)
	if cap(v) < n+1 {
		s := make(PackageImports, n, n*2+1)
		copy(s, v)
		v = s
	}

	v = v[0 : n+1]
	v[n] = PackageImport{alias, path}
	*pi = v
}

//-------------------------------------------------------------------------
// DeclFileCache
// 
// Contains cache for top-level declarations of a file as well as its
// contents, AST and import information. Used in both autocompletion
// and refactoring utilities.
//-------------------------------------------------------------------------

type DeclFileCache struct {
	name  string // file name
	mtime int64  // last modification time

	Data      []byte           // file contents
	File      *ast.File        // an AST tree
	Decls     map[string]*Decl // top-level declarations
	Error     os.Error         // last error
	Packages  PackageImports   // import information
	FileScope *Scope
}

func NewDeclFileCache(name string) *DeclFileCache {
	f := new(DeclFileCache)
	f.name = name
	return f
}

func (f *DeclFileCache) update() {
	stat, err := os.Stat(f.name)
	if err != nil {
		f.File = nil
		f.Data = nil
		f.Decls = nil
		f.Error = err
		return
	}

	if f.mtime == stat.Mtime_ns {
		return
	}

	f.mtime = stat.Mtime_ns
	f.readFile(f.name)
}

func (f *DeclFileCache) readFile(filename string) {
	f.Data, f.Error = ioutil.ReadFile(f.name)
	if f.Error != nil {
		return
	}

	f.processData()
}

func (f *DeclFileCache) processData() {
	f.File, f.Error = parser.ParseFile("", f.Data, 0)
	f.FileScope = NewScope(nil)
	anonymifyAst(f.File.Decls, 0, f.FileScope)
	f.Packages = NewPackageImports(f.name, f.File.Decls)
	f.Decls = make(map[string]*Decl, len(f.File.Decls))
	for _, decl := range f.File.Decls {
		appendToTopDecls(f.Decls, decl, f.FileScope)
	}
}

func (f *DeclFileCache) reprocess() {
	// drop mtime, because we're invalidating cache
	f.mtime = 0

	f.processData()
}

func appendToTopDecls(decls map[string]*Decl, decl ast.Decl, scope *Scope) {
	foreachDecl(decl, func(data *foreachDeclStruct) {
		class := astDeclClass(data.decl)
		for i, name := range data.names {
			typ, v, vi := data.typeValueIndex(i, 0)

			d := NewDecl2(name.Name, class, 0, typ, v, vi, scope)
			if d == nil {
				return
			}

			methodof := MethodOf(decl)
			if methodof != "" {
				decl, ok := decls[methodof]
				if ok {
					decl.AddChild(d)
				} else {
					decl = NewDecl(methodof, DECL_METHODS_STUB, scope)
					decls[methodof] = decl
					decl.AddChild(d)
				}
			} else {
				decl, ok := decls[d.Name]
				if ok {
					decl.ExpandOrReplace(d)
				} else {
					decls[d.Name] = d
				}
			}
		}
	})
}

func absPathForPackage(filename, p string) string {
	if p[0] == '.' {
		dir, _ := path.Split(filename)
		return fmt.Sprintf("%s.a", path.Join(dir, p))
	}
	return findGlobalFile(p)
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
	pkgfile := fmt.Sprintf("%s.a", imp)
	if Config.LibPath != "" {
		return path.Join(Config.LibPath, pkgfile)
	}

	goroot := os.Getenv("GOROOT")
	if goroot == "" {
		goroot = runtime.GOROOT()
	}
	goarch := os.Getenv("GOARCH")
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	goos := os.Getenv("GOOS")
	if goos == "" {
		goos = runtime.GOOS
	}
	pkgdir := fmt.Sprintf("%s_%s", goos, goarch)
	return path.Join(goroot, "pkg", pkgdir, pkgfile)
}

func packageName(file *ast.File) string {
	if file.Name != nil {
		return file.Name.Name
	}
	return ""
}

//-------------------------------------------------------------------------
// DeclCache
//
// Thread-safe collection of DeclFileCache entities.
//-------------------------------------------------------------------------

type DeclCache struct {
	cache map[string]*DeclFileCache
	sync.Mutex
}

func NewDeclCache() *DeclCache {
	c := new(DeclCache)
	c.cache = make(map[string]*DeclFileCache)
	return c
}

func (c *DeclCache) get(filename string) *DeclFileCache {
	c.Lock()
	defer c.Unlock()

	f, ok := c.cache[filename]
	if !ok {
		f = NewDeclFileCache(filename)
		c.cache[filename] = f
	}
	return f
}

func (c *DeclCache) Get(filename string) *DeclFileCache {
	f := c.get(filename)
	f.update()
	return f
}
