// +build !go1.9

package main

import (
	"go/ast"
)

func typeAliasSpec(name string, typ ast.Expr) *ast.TypeSpec {
	return &ast.TypeSpec{
		Name: ast.NewIdent(name),
		Type: typ,
	}
}

func isAliasTypeSpec(t *ast.TypeSpec) bool {
	return false
}
