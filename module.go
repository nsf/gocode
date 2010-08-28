package main

import (
	"strings"
	"bytes"
	"go/parser"
	"go/ast"
	"go/token"
	"go/scanner"
	"fmt"
	"os"
	"io/ioutil"
)

type ModuleCache struct {
	name     string
	filename string
	mtime    int64
	defalias string

	// used as a temporary for foreignifying package contents
	scope  *Scope
	main   *Decl // module declaration
	others map[string]*Decl

	// map which has that kind of entries:
	// ast -> go/ast
	// parser -> go/parser
	// etc.
	// used for replacing "" module names in .a files
	pathToAlias map[string]string
}

func NewModuleCache(name, filename string) *ModuleCache {
	m := new(ModuleCache)
	m.name = name
	m.filename = filename
	m.mtime = 0
	m.defalias = ""
	return m
}

func NewModuleCacheForever(name, defalias string) *ModuleCache {
	m := new(ModuleCache)
	m.name = name
	m.mtime = -1
	m.defalias = defalias
	return m
}

func (self *ModuleCache) updateCache() {
	if self.mtime == -1 {
		return
	}
	stat, err := os.Stat(self.filename)
	if err != nil {
		return
	}

	if self.mtime != stat.Mtime_ns {
		// clear tmp scope
		self.mtime = stat.Mtime_ns

		// try load new
		data, err := ioutil.ReadFile(self.filename)
		if err != nil {
			return
		}
		self.processPackageData(string(data))
	}
}

func (self *ModuleCache) processPackageData(s string) {
	self.scope = NewScope(nil)
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

	self.defalias = s[len("package ") : i-1]
	s = s[i+1:]

	self.pathToAlias = make(map[string]string)
	// hack, add ourselves to the module scope
	self.addFakeModuleToScope(self.defalias, self.name)
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
		decl2, pkg := self.processExport(decl)
		if len(decl2) == 0 {
			s = s[i+1:]
			continue
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
	self.others = make(map[string]*Decl)
	for key, value := range internalPackages {
		tmp := self.expandPackages(value.Bytes())
		decls, err := parser.ParseDeclList("", tmp)
		tmp = nil

		if err != nil {
			panic(fmt.Sprintf("failure in:\n%s\n%s\n", value, err.String()))
		} else {
			if key == self.name {
				// main package
				self.main = NewDecl(self.name, DECL_MODULE, nil)
				addAstDeclsToModule(self.main, decls, self.scope)
			} else {
				// others
				self.others[key] = NewDecl(key, DECL_MODULE, nil)
				addAstDeclsToModule(self.others[key], decls, self.scope)
			}
		}
	}
	self.pathToAlias = nil
	for key, value := range self.scope.entities {
		m, ok := self.others[value.Name]
		if !ok && value.Name == self.name {
			m = self.main
		}
		self.scope.entities[key] = m
	}
}

// feed one definition line from .a file here
// returns:
// 1. a go/parser parsable string representing one Go declaration
// 2. and a package name this declaration belongs to
func (self *ModuleCache) processExport(s string) (string, string) {
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

	switch s[b:e] {
	case "import":
		// skip import decls, we don't need them
		self.processImportStatement(s)
		return "", pkg
	case "const":
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

	// skip system functions (Init, etc.)
	i = strings.Index(s, "·")
	if i != -1 {
		return "", ""
	}

	if pkg == "" {
		pkg = self.name
	}

	return s, pkg
}

func (self *ModuleCache) processImportStatement(s string) {
	var scan scanner.Scanner
	scan.Init("", []byte(s), nil, 0)

	var alias, path string
	for i := 0; i < 3; i++ {
		_, tok, lit := scan.Scan()
		str := string(lit)
		switch tok {
		case token.IDENT:
			if str == "import" {
				continue
			} else {
				alias = str
			}
		case token.STRING:
			path = str[1 : len(str)-1]
		}
	}
	self.pathToAlias[path] = alias
	self.addFakeModuleToScope(alias, path)
}

func (self *ModuleCache) expandPackages(s []byte) []byte {
	out := bytes.NewBuffer(make([]byte, 0, len(s)))
	i := 0
	for {
		begin := i
		for i < len(s) && s[i] != '"' {
			i++
		}

		if i == len(s) {
			out.Write(s[begin:])
			return out.Bytes()
		}

		b := i // first '"'
		i++

		for i < len(s) && !(s[i] == '"' && s[i-1] != '\\') {
			i++
		}

		if i == len(s) {
			out.Write(s[begin:])
			return out.Bytes()
		}

		e := i // second '"'
		i++
		if s[b-1] == ':' {
			// special case, struct attribute literal, just remove ':'
			out.Write(s[begin : b-1])
			out.Write(s[b:i])
		} else if b+1 != e {
			// wow, we actually have something here
			alias, ok := self.pathToAlias[string(s[b+1:e])]
			if ok {
				out.Write(s[begin:b])
				out.Write([]byte(alias))
			} else {
				out.Write(s[begin:i])
			}
		} else {
			out.Write(s[begin:b])
			out.Write([]byte(self.defalias))
		}

	}
	panic("unreachable")
}

func (self *ModuleCache) addFakeModuleToScope(alias, realname string) {
	d := NewDecl(realname, DECL_MODULE, nil)
	self.scope.addDecl(alias, d)
}

func addAstDeclsToModule(module *Decl, decls []ast.Decl, scope *Scope) {
	for _, decl := range decls {
		foreachDecl(decl, func(decl ast.Decl, name string, value ast.Expr, valueindex int) {
			d := NewDeclFromAstDecl(name, DECL_FOREIGN, decl, value, valueindex, scope)
			if d == nil {
				return
			}

			if !ast.IsExported(name) {
				// We need types here, because embeddeing may
				// refer to unexported types which contain
				// exported methods, like in reflect package.
				if d.Class != DECL_TYPE {
					return
				}
			}

			methodof := MethodOf(decl)
			if methodof != "" {
				decl := module.FindChild(methodof)
				if decl != nil {
					decl.AddChild(d)
				} else {
					decl = NewDecl(methodof, DECL_METHODS_STUB, scope)
					decl.AddChild(d)
					module.AddChild(decl)
				}
			} else {
				decl := module.FindChild(d.Name)
				if decl != nil {
					decl.ExpandOrReplace(d)
				} else {
					module.AddChild(d)
				}
			}
		})
	}
}

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
		pkg = s[b+1 : e]
	}

	i += 2             // skip to a first symbol after dot
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
			pkg = s[b+1 : e]
		}

		i += 2             // skip to a first symbol after dot
		s = s[0:b] + s[i:] // strip package clause completely

		i = b
	}
	panic("unreachable")
	return "", ""
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

