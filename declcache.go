package main

import (
	"os"
	"strings"
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
				path,ok := absPathForPackage(filename, path)
				if ok {pi.appendImport(alias, path)}
			}
		} else {
			return
		}
	}
}

// Simple vector-like append.
func (pi *PackageImports) appendImport(alias, path string) {
	if alias == "_" {
		return
	}

	*pi = append(*pi, PackageImport{alias, path})
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

	Decls     map[string]*Decl // top-level declarations
	Error     os.Error         // last error
	Packages  PackageImports   // import information
	FileScope *Scope

	fset *token.FileSet
}

func NewDeclFileCache(name string) *DeclFileCache {
	f := new(DeclFileCache)
	f.name = name
	return f
}

func (f *DeclFileCache) update() {
	stat, err := os.Stat(f.name)
	if err != nil {
		f.Decls = nil
		f.Error = err
		f.fset = nil
		return
	}

	if f.mtime == stat.Mtime_ns {
		return
	}

	f.mtime = stat.Mtime_ns
	f.readFile(f.name)
}

func (f *DeclFileCache) readFile(filename string) {
	var data []byte
	data, f.Error = ioutil.ReadFile(f.name)
	if f.Error != nil {
		return
	}

	f.processData(data)
}

func (f *DeclFileCache) processData(data []byte) {
	var file *ast.File
	f.fset = token.NewFileSet()
	file, f.Error = parser.ParseFile(f.fset, "", data, 0)
	f.FileScope = NewScope(nil)
	for _, d := range file.Decls {
		anonymifyAst(d, 0, f.FileScope)
	}
	f.Packages = NewPackageImports(f.name, file.Decls)
	f.Decls = make(map[string]*Decl, len(file.Decls))
	for _, decl := range file.Decls {
		appendToTopDecls(f.Decls, decl, f.FileScope)
	}
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

func absPathForPackage(filename, p string) (string,bool) {
	dir, _ := path.Split(filename)
	if p[0] == '.' {
		return fmt.Sprintf("%s.a", path.Join(dir, p)),true
	}
	pkg,ok := findGoDagPackage(p,dir)
	if ok {return pkg,true}
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

func findGoDagPackage(imp,filedir string) (string,bool) {
	  // Support godag directory structure
		dir,pkg := path.Split(imp)
		godag_pkg := path.Join(filedir,"..",dir,"_obj",pkg+".a")
		if fileExists(godag_pkg) {return godag_pkg,true}
		return "",false
}

func findGlobalFile(imp string) (string,bool) {
	// gocode synthetically generates the builtin package
	// "unsafe", since the "unsafe.a" package doesn't really exist.
	// Thus, when the user request for the package "unsafe" we
	// would return synthetic global file that would be used
	// just as a key name to find this synthetic package
	if imp == "unsafe" {return "unsafe",true}

	pkgfile := fmt.Sprintf("%s.a", imp)

	// if lib-path is defined, use it
	if Config.LibPath != "" {
		for _,p := range strings.Split(Config.LibPath,":",-1) {
			pkg_path := path.Join(p, pkgfile)
			if fileExists(pkg_path) {return pkg_path,true}
		}
		return "",false
	}

	// otherwise figure out the default lib-path
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
	pkg_path := path.Join(goroot, "pkg", pkgdir, pkgfile)
	return pkg_path,fileExists(pkg_path)
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
