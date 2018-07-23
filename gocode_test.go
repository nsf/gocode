package main

import (
	"go/build"
	"testing"
)

func TestGocode(t *testing.T) {
	var ctx package_lookup_context
	ctx.Context = build.Default
	*g_debug = true
	resolveKnownPackageIdent("fmt", "gocode.go", &ctx)
	resolveKnownPackageIdent("os", "gocode.go", &ctx)
	resolveKnownPackageIdent("http", "gocode.go", &ctx)
}
