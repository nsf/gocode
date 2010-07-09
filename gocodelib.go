package main

import (
	"fmt"
	"bytes"
	"reflect"
	"go/parser"
	"go/ast"
	"go/token"
	"strings"
	"io/ioutil"
	"io"
	"os"
)

// TODO: probably change hand-written string literals processing to a
// "scanner"-based one

func skipSpaces(i int, s string) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func skipToSpace(i int, s string) int {
	for i < len(s) && s[i] != ' ' && s[i] != '\t' {
		i++
	}
	return i
}

// convert package name to a nice ident, e.g.: "go/ast" -> "ast"
func identifyPackage(s string) string {
	i := len(s)-1

	// 'i > 0' is correct here, because we should never see '/' at the
	// beginning of the name anyway
	for ; i > 0; i-- {
		if s[i] == '/' {
			break
		}
	}
	if s[i] == '/' {
		return s[i+1:]
	}
	return s
}

func extractPackage(i int, s string) (string, string) {
	pkg := ""

	b := i // first '"'
	i++

	for i < len(s) && s[i] != '"' {
		i++
	}

	if i == len(s) {
		return s, pkg
	}

	e := i // second '"'
	if b+1 != e {
		// wow, we actually have something here
		pkg = s[b+1:e]
	}

	i += 2 // skip to a first symbol after dot
	s = s[0:b] + s[i:] // strip package clause completely

	return s, pkg
}

// returns modified 's' with package stripped from the method and the package name
func extractPackageFromMethod(i int, s string) (string, string) {
	pkg := ""
	for {
		for i < len(s) && s[i] != ')' && s[i] != '"' {
			i++
		}

		if s[i] == ')' || i == len(s) {
			return s, pkg
		}

		b := i // first '"'
		i++

		for i < len(s) && s[i] != ')' && s[i] != '"' {
			i++
		}

		if s[i] == ')' || i == len(s) {
			return s, pkg
		}

		e := i // second '"'
		if b+1 != e {
			// wow, we actually have something here
			pkg = s[b+1:e]
		}

		i += 2 // skip to a first symbol after dot
		s = s[0:b] + s[i:] // strip package clause completely

		i = b
	}
	panic("unreachable")
	return "", ""
}

func expandPackages(s string) string {
	i := 0
	for {
		pkg := ""
		for i < len(s) && s[i] != '"' && s[i] != '=' {
			i++
		}

		if i == len(s) || s[i] == '=' {
			return s
		}

		b := i // first '"'
		i++

		for i < len(s) && !(s[i] == '"' && s[i-1] != '\\') && s[i] != '=' {
			i++
		}

		if i == len(s) || s[i] == '=' {
			return s
		}

		e := i // second '"'
		if s[b-1] == ':' {
			// special case, struct attribute literal, just remove ':'
			s = s[0:b-1] + s[b:]
			i = e
		} else if b+1 != e {
			// wow, we actually have something here
			pkg = identifyPackage(s[b+1:e])
			i++ // skip to a first symbol after second '"'
			s = s[0:b] + pkg + s[i:] // strip package clause completely
			i = b
		} else {
			i += 2
			s = s[0:b] + s[i:]
			i = b
		}

	}
	panic("unreachable")
	return ""
}

func preprocessConstDecl(s string) string {
	i := strings.Index(s, "=")
	if i == -1 {
		return s
	}

	for i < len(s) && !(s[i] >= '0' && s[i] <= '9') && s[i] != '"' && s[i] != '\'' {
		i++
	}

	if i == len(s) || s[i] == '"' || s[i] == '\'' {
		return s
	}

	// ok, we have a digit!
	b := i
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == 'p' || s[i] == '-' || s[i] == '+') {
		i++
	}
	e := i

	return s[0:b] + "0" + s[e:]
}

// feed one definition line from .a file here
// returns:
// 1. a go/parser parsable string representing one Go declaration
// 2. and a package name this declaration belongs to
func processExport(s string) (string, string) {
	i := 0
	pkg := ""

	// skip to a decl type: (type | func | const | var | import)
	i = skipSpaces(i, s)
	if i == len(s) {
		return "", pkg
	}
	b := i
	i = skipToSpace(i, s)
	if i == len(s) {
		return "", pkg
	}
	e := i

	// skip import and const(TODO) decls, we don't need them
	if s[b:e] == "import" {
		return "", pkg
	} else if s[b:e] == "const" {
		s = preprocessConstDecl(s)
	}
	i++ // skip space after a decl type

	// extract a package this decl belongs to
	switch s[i] {
	case '(':
		s, pkg = extractPackageFromMethod(i, s)
	case '"':
		s, pkg = extractPackage(i, s)
	}

	// make everything parser friendly
	s = strings.Replace(s, "?", "", -1)
	s = expandPackages(s)

	// skip system functions (Init, etc.)
	i = strings.Index(s, "·")
	if i != -1 {
		return "", ""
	}

	return s, pkg
}

func (self *AutoCompleteContext) processPackage(filename string, uniquename string, pkgname string) {
	if self.cache[filename] {
		return
	}
	self.cache[filename] = true
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		panic("Failed to open archive file")
	}
	s := string(data)

	i := strings.Index(s, "import\n$$\n")
	if i == -1 {
		panic("Can't find the import section in the archive file")
	}
	s = s[i+len("import\n$$\n"):]
	i = strings.Index(s, "$$\n")
	if i == -1 {
		panic("Can't find the end of the import section in the archive file")
	}
	s = s[0:i] // leave only import section

	i = strings.Index(s, "\n")
	if i == -1 {
		panic("Wrong file")
	}

	if pkgname == "" {
		pkgname = s[len("package "):i-1]
	}
	self.AddAlias(pkgname, uniquename)
	if self.debuglog != nil {
		fmt.Fprintf(self.debuglog, "parsing package '%s'...\n", pkgname)
	}
	s = s[i+1:]

	internalPackages := make(map[string]*bytes.Buffer)
	for {
		// for each line
		i := strings.Index(s, "\n")
		if i == -1 {
			break
		}
		decl := strings.TrimSpace(s[0:i])
		if len(decl) == 0 {
			s = s[i+1:]
			continue
		}
		decl2, pkg := processExport(decl)
		if len(decl2) == 0 {
			s = s[i+1:]
			continue
		}

		if pkg == "" {
			// local package, use ours name
			pkg = uniquename
		}

		buf := internalPackages[pkg]
		if buf == nil {
			buf = bytes.NewBuffer(make([]byte, 0, 4096))
			internalPackages[pkg] = buf
		}
		buf.WriteString(decl2)
		buf.WriteString("\n")
		s = s[i+1:]
	}
	for key, value := range internalPackages {
		decls, err := parser.ParseDeclList("", value.Bytes(), nil)
		if err != nil {
			panic(fmt.Sprintf("failure in:\n%s\n%s\n", value, err.String()))
		} else {
			if self.debuglog != nil {
				fmt.Fprintf(self.debuglog, "\t%s: OK (ndecls: %d)\n", key, len(decls))
			}
			f := new(ast.File) // fake file
			f.Decls = decls
			ast.FileExports(f)
			self.Add(key, f.Decls)
		}
	}
}

func prettyPrintTypeExpr(out io.Writer, e ast.Expr) {
	ty := reflect.Typeof(e)
	if false {
		fmt.Fprintf(out, "%s\n", ty.String())
	}
	switch t := e.(type) {
	case *ast.StarExpr:
		fmt.Fprintf(out, "*")
		prettyPrintTypeExpr(out, t.X)
	case *ast.Ident:
		fmt.Fprintf(out, t.Name())
	case *ast.ArrayType:
		fmt.Fprintf(out, "[]")
		prettyPrintTypeExpr(out, t.Elt)
	case *ast.SelectorExpr:
		prettyPrintTypeExpr(out, t.X)
		fmt.Fprintf(out, ".%s", t.Sel.Name())
	case *ast.FuncType:
		fmt.Fprintf(out, "func(")
		prettyPrintFuncFieldList(out, t.Params)
		fmt.Fprintf(out, ")")

		buf := bytes.NewBuffer(make([]byte, 0, 256))
		nresults := prettyPrintFuncFieldList(buf, t.Results)
		if nresults > 0 {
			results := buf.String()
			if strings.Index(results, " ") != -1 {
				results = "(" + results + ")"
			}
			fmt.Fprintf(out, " %s", results)
		}
	case *ast.MapType:
		fmt.Fprintf(out, "map[")
		prettyPrintTypeExpr(out, t.Key)
		fmt.Fprintf(out, "]")
		prettyPrintTypeExpr(out, t.Value)
	case *ast.InterfaceType:
		fmt.Fprintf(out, "interface{}")
	case *ast.Ellipsis:
		fmt.Fprintf(out, "...")
		prettyPrintTypeExpr(out, t.Elt)
	default:
		fmt.Fprintf(out, "\n[!!] unknown type: %s\n", ty.String())
	}
}

func prettyPrintFuncFieldList(out io.Writer, f *ast.FieldList) int {
	count := 0
	if f == nil {
		return count
	}
	for i, field := range f.List {
		// names
		if field.Names != nil {
			for j, name := range field.Names {
				fmt.Fprintf(out, "%s", name.Name())
				if j != len(field.Names)-1 {
					fmt.Fprintf(out, ", ")
				}
				count++
			}
			fmt.Fprintf(out, " ")
		} else {
			count++
		}

		// type
		prettyPrintTypeExpr(out, field.Type)

		// ,
		if i != len(f.List)-1 {
			fmt.Fprintf(out, ", ")
		}
	}
	return count
}

func startsWith(s, prefix string) bool {
	if len(s) >= len(prefix) && s[0:len(prefix)] == prefix {
		return true
	}
	return false
}

func prettyPrintDecl(out io.Writer, d ast.Decl, p string) {
	switch t := d.(type) {
	case *ast.GenDecl:
		switch t.Tok {
		case token.CONST:
			for _, spec := range t.Specs {
				c := spec.(*ast.ValueSpec)
				for _, name := range c.Names {
					if p != "" && !startsWith(name.Name(), p) {
						continue
					}
					fmt.Fprintf(out, "const %s\n", name.Name())
				}
			}
		case token.TYPE:
			for _, spec := range t.Specs {
				t := spec.(*ast.TypeSpec)
				if p != "" && !startsWith(t.Name.Name(), p) {
					continue
				}
				fmt.Fprintf(out, "type %s\n", t.Name.Name())
			}
		case token.VAR:
			for _, spec := range t.Specs {
				v := spec.(*ast.ValueSpec)
				for _, name := range v.Names {
					if p != "" && !startsWith(name.Name(), p) {
						continue
					}
					fmt.Fprintf(out, "var %s\n", name.Name())
				}
			}
		default:
			fmt.Fprintf(out, "\tgen STUB\n")
		}
	case *ast.FuncDecl:
		if t.Recv != nil {
			//XXX: skip method, temporary
			break
		}
		if p != "" && !startsWith(t.Name.Name(), p) {
			break
		}
		fmt.Fprintf(out, "func %s(", t.Name.Name())
		prettyPrintFuncFieldList(out, t.Type.Params)
		fmt.Fprintf(out, ")")

		buf := bytes.NewBuffer(make([]byte, 0, 256))
		nresults := prettyPrintFuncFieldList(buf, t.Type.Results)
		if nresults > 0 {
			results := buf.String()
			if strings.Index(results, " ") != -1 {
				results = "(" + results + ")"
			}
			fmt.Fprintf(out, " %s", results)
		}
		fmt.Fprintf(out, "\n")
	}
}

func autoCompleteDecl(out io.Writer, d ast.Decl, p string) {
	switch t := d.(type) {
	case *ast.GenDecl:
		switch t.Tok {
		case token.CONST:
			for _, spec := range t.Specs {
				c := spec.(*ast.ValueSpec)
				for _, name := range c.Names {
					if p != "" && !startsWith(name.Name(), p) {
						continue
					}
					fmt.Fprintf(out, "%s\n", name.Name()[len(p):])
				}
			}
		case token.TYPE:
			for _, spec := range t.Specs {
				t := spec.(*ast.TypeSpec)
				if p != "" && !startsWith(t.Name.Name(), p) {
					continue
				}
				fmt.Fprintf(out, "%s\n", t.Name.Name()[len(p):])
			}
		case token.VAR:
			for _, spec := range t.Specs {
				v := spec.(*ast.ValueSpec)
				for _, name := range v.Names {
					if p != "" && !startsWith(name.Name(), p) {
						continue
					}
					fmt.Fprintf(out, "%s\n", name.Name()[len(p):])
				}
			}
		default:
			fmt.Fprintf(out, "[!!] STUB\n")
		}
	case *ast.FuncDecl:
		if t.Recv != nil {
			//XXX: skip method, temporary
			break
		}
		if p != "" && !startsWith(t.Name.Name(), p) {
			break
		}
		fmt.Fprintf(out, "%s(\n", t.Name.Name()[len(p):])
	}
}

func findFile(imp string) string {
	goroot := os.Getenv("GOROOT")
	goarch := os.Getenv("GOARCH")
	goos := os.Getenv("GOOS")

	return fmt.Sprintf("%s/pkg/%s_%s/%s.a", goroot, goos, goarch, imp)
}

func (self *AutoCompleteContext) processData(data []byte) {
	file, err := parser.ParseFile("", data, nil, parser.ImportsOnly)
	if err != nil {
		panic(err.String())
	}

	decl, ok := file.Decls[0].(*ast.GenDecl)
	if !ok {
		panic("Fail")
	}

	for _, spec := range decl.Specs {
		imp, ok := spec.(*ast.ImportSpec)
		if !ok {
			panic("Fail")
		}

		s := string(imp.Path.Value)
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name()
		}
		s = s[1:len(s)-1]
		self.processPackage(findFile(s), s, alias)
	}
}

type AutoCompleteContext struct {
	// main map:
	// unique package name ->
	//	[]ast.Decl
	m map[string][]ast.Decl
	// TODO: ast.Decl is a bit evil here, we need our own stuff

	// current file namespace:
	// alias name ->
	//	unique package name
	cfns map[string]string
	cache map[string]bool
	debuglog io.Writer
}

func NewAutoCompleteContext() *AutoCompleteContext {
	self := new(AutoCompleteContext)
	self.m = make(map[string][]ast.Decl)
	self.cfns = make(map[string]string)
	self.cache = make(map[string]bool)
	return self
}

func (self *AutoCompleteContext) Add(globalname string, decls []ast.Decl) {
	self.m[globalname] = decls
}

func (self *AutoCompleteContext) AddAlias(alias string, globalname string) {
	self.cfns[alias] = globalname
}

func (self *AutoCompleteContext) Apropos(file []byte, apropos string) ([]string, []string) {
	self.processData(file)

	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	buf2 := bytes.NewBuffer(make([]byte, 0, 4096))

	parts := strings.Split(apropos, ".", 2)
	switch len(parts) {
	case 1:
		for _, decl := range self.m[self.cfns[apropos]] {
			prettyPrintDecl(buf, decl, "")
			autoCompleteDecl(buf2, decl, "")
		}
	case 2:
		for _, decl := range self.m[self.cfns[parts[0]]] {
			prettyPrintDecl(buf, decl, parts[1])
			autoCompleteDecl(buf2, decl, parts[1])
		}
	}

	result := strings.Split(buf.String(), "\n", -1)
	result2 := strings.Split(buf2.String(), "\n", -1)
	return result, result2
}
