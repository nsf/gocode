package main

import (
	"go/ast"
	"go/parser"
	"go/token"
)

type highlight_input_file struct {
	name         string
	package_name string
	env    *gocode_env
	fset   *token.FileSet
}

type highlight_range struct {
	Format  string
	Line   int32
	Column int32
	Length  int32
}

type highlight_context struct {
	current *highlight_input_file
}

func new_highlight_input_file(name string, env *gocode_env) *highlight_input_file {
	p := new(highlight_input_file)
	p.name = name
	p.fset = token.NewFileSet()
	p.env = env
	return p
}

func new_highlight_context(env *gocode_env) *highlight_context {
	c := new(highlight_context)
	c.current = new_highlight_input_file("", env)
	return c
}

func (c *highlight_context) find_ranges(file []byte, filename string) (ranges []highlight_range, d int) {
	c.current.name = filename

	// Parse whole file.
	parsedAst, _ := parser.ParseFile(c.current.fset, "", file, 0)
	c.current.package_name = package_name(parsedAst)

	var v = new_highlight_visitor(c, parsedAst)
	ast.Walk(v, parsedAst)

	return v.ranges, 0
}
