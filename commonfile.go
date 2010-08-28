package main

import (
	"go/ast"
	"go/token"
	"path"
	"fmt"
	"os"
)

type moduleImport struct {
	alias  string
	name   string
	path   string
	module *Decl
}

type commonFile struct {
	name        string
	packageName string
	mtime       int64

	// cache
	modules   []moduleImport
	filescope *Scope
	scope *Scope
}

func (c *commonFile) resetCache() {
	c.modules = make([]moduleImport, 0, 16)
	c.filescope = NewScope(nil)
	c.scope = c.filescope
}

func (c *commonFile) addModuleImport(alias, path string) {
	if alias == "_" || alias == "." {
		// TODO: support for modules imported in the current namespace
		return
	}
	name := path
	path = c.findFile(path)
	if name[0] == '.' {
		// use file path for local packages as name
		name = path
	}

	n := len(c.modules)
	if cap(c.modules) < n+1 {
		s := make([]moduleImport, n, n*2+1)
		copy(s, c.modules)
		c.modules = s
	}

	c.modules = c.modules[0 : n+1]
	c.modules[n] = moduleImport{alias, name, path, nil}
}

func (c *commonFile) processImports(decls []ast.Decl) {
	for _, decl := range decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			for _, spec := range gd.Specs {
				imp, ok := spec.(*ast.ImportSpec)
				if !ok {
					panic("Fail")
				}
				c.processImportSpec(imp)
			}
		} else {
			return
		}
	}
}

func (c *commonFile) applyImports() {
	for _, mi := range c.modules {
		c.filescope.addDecl(mi.alias, mi.module)
	}
}

func (c *commonFile) processImportSpec(imp *ast.ImportSpec) {
	path, alias := pathAndAlias(imp)

	// add module to a cache
	c.addModuleImport(alias, path)
}

func (c *commonFile) findFile(imp string) string {
	if imp[0] == '.' {
		dir, _ := path.Split(c.name)
		return fmt.Sprintf("%s.a", path.Join(dir, imp))
	}
	return findGlobalFile(imp)
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
	goroot := os.Getenv("GOROOT")
	goarch := os.Getenv("GOARCH")
	goos := os.Getenv("GOOS")

	pkgdir := fmt.Sprintf("%s_%s", goos, goarch)
	pkgfile := fmt.Sprintf("%s.a", imp)

	return path.Join(goroot, "pkg", pkgdir, pkgfile)
}

func packageName(file *ast.File) string {
	if file.Name != nil {
		return file.Name.Name
	}
	return ""
}

