package main

import (
	"fmt"
	"go/parser"
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

func processPackage(filename string) {
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
	pkgname := s[len("package "):i-1]
	fmt.Printf("parsing package '%s'\n", pkgname)
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
			pkg = "__this__"
		}

		if _, ok := internalPackages[pkg]; ok {
			internalPackages[pkg] = internalPackages[pkg] + decl2 + "\n"
		} else {
			internalPackages[pkg] = decl2 + "\n"
		}
		/*decls, err := parser.ParseDeclList("", decl2 + "\n", nil)
		if err != nil {
			fmt.Printf("!!!!!!!!!!!!!!!! FAILURE !!!!!!!!!!!!!!!!\n%s\n", decl2)
			panic(fmt.Sprintf("%s\n", err.String()))
		} else {
			fmt.Printf("OK")
		}
		*/
		s = s[i+1:]

		//fmt.Printf("\t%s\t%s\n", pkg, decl2)
	}

	for key, value := range internalPackages {
		decls, err := parser.ParseDeclList("", value, nil)
		if err != nil {
			fmt.Printf("!!!!!!!!!!!!!!!! FAILURE !!!!!!!!!!!!!!!!\n%s\n", value)
			panic(fmt.Sprintf("%s\n", err.String()))
		} else {
			fmt.Printf("\t%s: \033[32mOK\033[0m (ndecls: %d)\n", key, len(decls))
		}
	}
}

func main() {
	for _, arg := range os.Args[1:] {
		processPackage(arg)
	}
	fmt.Printf("Total number of packages: %d\n", len(os.Args)-1)
}
