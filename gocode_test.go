package main

import (
	"fmt"
	"go/build"
	"io/ioutil"
	"log"
	"os"
	"testing"
)

func _TestGocode(t *testing.T) {
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
	autocomplete := new_auto_complete_context(&ctx, pkgcache, declcache)
	data, err := ioutil.ReadFile("package_types.go")
	if err != nil {
		t.Fatal(err)
	}
	ar, n := autocomplete.apropos(data, "./package_types.go", 714)
	fmt.Println(ar, n)
}

func test1(a string) {

}

func TestModule(t *testing.T) {
	//*g_debug = true

	d := &daemon{}
	d.pkgcache = new_package_cache()
	d.declcache = new_decl_cache(&d.context)
	d.autocomplete = new_auto_complete_context(&d.context, d.pkgcache, d.declcache)
	g_daemon = d

	//ar, n := test_auto_complete(&build.Default, "./package_types.go", 1809)
	//ar, n := test_auto_complete(&build.Default, "./server.go", 1443)
	//test_auto_complete(&build.Default, "./server.go", 1443)

	//ar, n =
	ar, n := test_auto_complete(&build.Default, "/Users/vfc/go/vtest/main.go", 567)
	test_auto_complete(&build.Default, "/Users/vfc/go/vtest/main.go", 568)
	fmt.Println(ar, n)
	//test_auto_complete(&build.Default, "/Users/vfc/go/vtest/main.go", 550)
	//test_auto_complete(&build.Default, "/Users/vfc/go/vtest/main.go", 550)
	//ar, n := test_auto_complete(&build.Default, "/Users/vfc/dev/liteide/liteidex/src/github.com/visualfc/gotools/main.go", 1449)
	//fmt.Println(ar, n)
	//	p, up, _ := d.autocomplete.walker.Check("/Users/vfc/go/vtest", nil)
	//	log.Println(p, up)
	//	p, up, _ = d.autocomplete.walker.Check(".", nil)
	//	log.Println(p, up)
}

func testv(a string) {

}

func test_auto_complete(ctx *build.Context, filename string, pos int) (c []candidate, d int) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalln(err)
	}
	return server_auto_complete(data, filename, pos, pack_build_context(ctx))
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
