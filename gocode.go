package main

import (
	"fmt"
	"go/parser"
	"go/ast"
	"go/token"
	"strings"
	"io/ioutil"
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

	return s[0:b-1] + "0" + s[e:]
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
	//fmt.Printf("parsing package '%s'...\n", pkgname)
	s = s[i+1:]

	internalPackages := make(map[string]string, 50)
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

		internalPackages[pkg] = internalPackages[pkg] + decl2 + "\n"
		s = s[i+1:]
	}

	for key, value := range internalPackages {
		decls, err := parser.ParseDeclList("", value, nil)
		if err != nil {
			fmt.Printf("!!!!!!!!!!!!!!!! FAILURE !!!!!!!!!!!!!!!!\n%s\n", value)
			panic(fmt.Sprintf("%s\n", err.String()))
		} else {
			//fmt.Printf("\t%s: \033[32mOK\033[0m (ndecls: %d)\n", key, len(decls))
			f := new(ast.File) // fake file
			f.Decls = decls
			ast.FileExports(f)
			self.Add(key, f.Decls)
		}
	}
}

func prettyPrintDecl(d ast.Decl, p string) {
	switch t := d.(type) {
	case *ast.GenDecl:
		switch t.Tok {
		case token.CONST:
			for _, spec := range t.Specs {
				c := spec.(*ast.ValueSpec)
				for _, name := range c.Names {
					if len(name.Name()) < len(p) || (p != "" && name.Name()[0:len(p)] != p) {
						continue
					}
					fmt.Printf("\tconst %s\n", name.Name())
				}
			}
		case token.TYPE:
			for _, spec := range t.Specs {
				t := spec.(*ast.TypeSpec)
				if len(t.Name.Name()) < len(p) || (p != "" && t.Name.Name()[0:len(p)] != p) {
					continue
				}
				fmt.Printf("\ttype %s\n", t.Name.Name())
			}
		case token.VAR:
			for _, spec := range t.Specs {
				v := spec.(*ast.ValueSpec)
				for _, name := range v.Names {
					if len(name.Name()) < len(p) || (p != "" && name.Name()[0:len(p)] != p) {
						continue
					}
					fmt.Printf("\tvar %s\n", name.Name())
				}
			}
		default:
			fmt.Printf("\tgen STUB\n")
		}
	case *ast.FuncDecl:
		if t.Recv != nil {
			//XXX: skip method, temporary
			break
		}
		if len(t.Name.Name()) < len(p) || (p != "" && t.Name.Name()[0:len(p)] != p) {
			break
		}
		fmt.Printf("\tfunc %s\n", t.Name.Name())
	}
}

func findFile(imp string) string {
	goroot := os.Getenv("GOROOT")
	goarch := os.Getenv("GOARCH")
	goos := os.Getenv("GOOS")

	return fmt.Sprintf("%s/pkg/%s_%s/%s.a", goroot, goos, goarch, imp)
}

func (self *AutoCompleteContext) processEndFile(filename string) {
	file, err := parser.ParseFile(filename, nil, nil, parser.ImportsOnly)
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
}

func NewAutoCompleteContext() *AutoCompleteContext {
	self := new(AutoCompleteContext)
	self.m = make(map[string][]ast.Decl)
	self.cfns = make(map[string]string)
	return self
}

func (self *AutoCompleteContext) Add(globalname string, decls []ast.Decl) {
	self.m[globalname] = decls
}

func (self *AutoCompleteContext) AddAlias(alias string, globalname string) {
	self.cfns[alias] = globalname
}

func main() {
	if len(os.Args) < 2 {
		panic("usage: ./gocode <go file> <apropos request>")
	}
	ctx := NewAutoCompleteContext()
	ctx.processEndFile(os.Args[1])

	switch len(os.Args) {
	case 2:
		for alias, pkgid := range ctx.cfns {
			decls := ctx.m[pkgid]
			fmt.Printf("package: '%s'...\n", alias)
			for _, decl := range decls {
				prettyPrintDecl(decl, "")
			}
		}
	case 3:
		request := os.Args[2]
		parts := strings.Split(request, ".", 2)
		switch len(parts) {
		case 1:
			for _, decl := range ctx.m[ctx.cfns[request]] {
				prettyPrintDecl(decl, "")
			}
		case 2:
			for _, decl := range ctx.m[ctx.cfns[parts[0]]] {
				prettyPrintDecl(decl, parts[1])
			}
		}
	}
}
