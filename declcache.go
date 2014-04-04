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
// []packageImport
//-------------------------------------------------------------------------

type packageImport struct {
	alias string
	path  string
}

// it's defined here, simply because packageImport is the only user
type gocodeEnv struct {
	GOPATH string
	GOROOT string
	GOARCH string
	GOOS   string
}

func (env *gocodeEnv) get() {
	env.GOPATH = os.Getenv("GOPATH")
	env.GOROOT = os.Getenv("GOROOT")
	env.GOARCH = os.Getenv("GOARCH")
	env.GOOS = os.Getenv("GOOS")
	if env.GOROOT == "" {
		env.GOROOT = runtime.GOROOT()
	}
	if env.GOARCH == "" {
		env.GOARCH = runtime.GOARCH
	}
	if env.GOOS == "" {
		env.GOOS = runtime.GOOS
	}
}

// Parses import declarations until the first non-import declaration and fills
// `packages` array with import information.
func collectPackageImports(filename string, decls []ast.Decl, env *gocodeEnv) []packageImport {
	pi := make([]packageImport, 0, 16)
	for _, decl := range decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			for _, spec := range gd.Specs {
				imp := spec.(*ast.ImportSpec)
				path, alias := pathAndAlias(imp)
				path, ok := absPathForPackage(filename, path, env)
				if ok && alias != "_" {
					pi = append(pi, packageImport{alias, path})
				}
			}
		} else {
			break
		}
	}
	return pi
}

//-------------------------------------------------------------------------
// declFileCache
//
// Contains cache for top-level declarations of a file as well as its
// contents, AST and import information.
//-------------------------------------------------------------------------

type declFileCache struct {
	name  string // file name
	mtime int64  // last modification time

	decls     map[string]*decl // top-level declarations
	error     error            // last error
	packages  []packageImport // import information
	filescope *scope

	fset *token.FileSet
	env  *gocodeEnv
}

func newDeclFileCache(name string, env *gocodeEnv) *declFileCache {
	return &declFileCache{
		name: name,
		env:  env,
	}
}

func (f *declFileCache) update() {
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
	f.readFile()
}

func (f *declFileCache) readFile() {
	var data []byte
	data, f.error = fileReader.readFile(f.name)
	if f.error != nil {
		return
	}
	data, _ = filterOutShebang(data)

	f.processData(data)
}

func (f *declFileCache) processData(data []byte) {
	var file *ast.File
	f.fset = token.NewFileSet()
	file, f.error = parser.ParseFile(f.fset, "", data, 0)
	f.filescope = newScope(nil)
	for _, d := range file.Decls {
		anonymifyAst(d, 0, f.filescope)
	}
	f.packages = collectPackageImports(f.name, file.Decls, f.env)
	f.decls = make(map[string]*decl, len(file.Decls))
	for _, decl := range file.Decls {
		appendToTopDecls(f.decls, decl, f.filescope)
	}
}

func appendToTopDecls(decls map[string]*decl, decl ast.Decl, scope *scope) {
	foreachDecl(decl, func(data *foreachDeclStruct) {
		class := astDeclClass(data.decl)
		for i, name := range data.names {
			typ, v, vi := data.typeValueIndex(i)

			d := newDeclFull(name.Name, class, 0, typ, v, vi, scope)
			if d == nil {
				return
			}

			methodof := methodOf(decl)
			if methodof != "" {
				decl, ok := decls[methodof]
				if ok {
					decl.addChild(d)
				} else {
					decl = newDecl(methodof, declMethodsStub, scope)
					decls[methodof] = decl
					decl.addChild(d)
				}
			} else {
				decl, ok := decls[d.name]
				if ok {
					decl.expandOrReplace(d)
				} else {
					decls[d.name] = d
				}
			}
		}
	})
}

func absPathForPackage(filename, p string, env *gocodeEnv) (string, bool) {
	dir, _ := filepath.Split(filename)
	if len(p) == 0 {
		return "", false
	}
	if p[0] == '.' {
		return fmt.Sprintf("%s.a", filepath.Join(dir, p)), true
	}
	pkg, ok := findGoDagPackage(p, dir)
	if ok {
		return pkg, true
	}
	return findGlobalFile(p, env)
}

func pathAndAlias(imp *ast.ImportSpec) (string, string) {
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

func findGoDagPackage(imp, filedir string) (string, bool) {
	// Support godag directory structure
	dir, pkg := filepath.Split(imp)
	godagPkg := filepath.Join(filedir, "..", dir, "Obj", pkg+".a")
	if fileExists(godagPkg) {
		return godagPkg, true
	}
	return "", false
}

func findGlobalFile(imp string, env *gocodeEnv) (string, bool) {
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
	if gConfig.LibPath != "" {
		for _, p := range filepath.SplitList(gConfig.LibPath) {
			pkgPath := filepath.Join(p, pkgfile)
			if fileExists(pkgPath) {
				return pkgPath, true
			}
			// Also check the relevant pkg/OS_ARCH dir for the libpath, if provided.
			pkgdir := fmt.Sprintf("%s_%s", env.GOOS, env.GOARCH)
			pkgPath = filepath.Join(p, "pkg", pkgdir, pkgfile)
			if fileExists(pkgPath) {
				return pkgPath, true
			}
		}
	}

	pkgdir := fmt.Sprintf("%s_%s", env.GOOS, env.GOARCH)
	pkgpath := filepath.Join("pkg", pkgdir, pkgfile)

	if env.GOPATH != "" {
		for _, p := range filepath.SplitList(env.GOPATH) {
			gopathPkg := filepath.Join(p, pkgpath)
			if fileExists(gopathPkg) {
				return gopathPkg, true
			}
		}
	}
	gorootPkg := filepath.Join(env.GOROOT, pkgpath)
	return gorootPkg, fileExists(gorootPkg)
}

func packageName(file *ast.File) string {
	if file.Name != nil {
		return file.Name.Name
	}
	return ""
}

//-------------------------------------------------------------------------
// declCache
//
// Thread-safe collection of DeclFileCache entities.
//-------------------------------------------------------------------------

type declCache struct {
	cache map[string]*declFileCache
	env   *gocodeEnv
	sync.Mutex
}

func newDeclCache(env *gocodeEnv) *declCache {
	return &declCache{
		cache: make(map[string]*declFileCache),
		env:   env,
	}
}

func (c *declCache) get(filename string) *declFileCache {
	c.Lock()
	defer c.Unlock()

	f, ok := c.cache[filename]
	if !ok {
		f = newDeclFileCache(filename, c.env)
		c.cache[filename] = f
	}
	return f
}

func (c *declCache) getAndUpdate(filename string) *declFileCache {
	f := c.get(filename)
	f.update()
	return f
}
