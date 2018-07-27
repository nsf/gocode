package main

import (
	"fmt"
	"go/build"
	"io/ioutil"
	"os"
	"testing"
)

func TestGocode(t *testing.T) {
	var ctx package_lookup_context
	ctx.Context = build.Default
	dir, _ := os.Getwd()
	bp, err := build.ImportDir(dir, build.FindOnly)
	if err != nil {
		return
	}
	ctx.CurrentPackagePath = bp.ImportPath

	*g_debug = true
	g_config.Autobuild = false
	//resolveKnownPackageIdent("fmt", "gocode.go", &ctx)
	//resolveKnownPackageIdent("os", "gocode.go", &ctx)
	//resolveKnownPackageIdent("http", "gocode.go", &ctx)
	//resolvePackageIdent("github.com/visualfc/gotools/pkg/srcimporter", "package_types.go", &ctx)
	//return
	declcache := new_decl_cache(&ctx)
	pkgcache := new_package_cache()
	autocomplete := new_auto_complete_context(pkgcache, declcache)
	data, err := ioutil.ReadFile("package_types.go")
	if err != nil {
		t.Fatal(err)
	}
	ar, n := autocomplete.apropos(data, "./package_types.go", 714)
	fmt.Println(ar, n)
}

func resolvePackageIdent(importPath string, filename string, c *auto_complete_context, context *package_lookup_context) *package_file_cache {
	pkg, vname, ok := abs_path_for_package(filename, importPath, context)
	if !ok {
		return nil
	}
	p := new_package_file_cache(pkg, importPath, vname)
	p.update_cache(c)
	return p
}
