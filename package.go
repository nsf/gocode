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
	"regexp"
)

//-------------------------------------------------------------------------
// PackageFileCache
//
// Structure that represents a cache for an imported pacakge. In other words
// these are the contents of an archive (*.a) file.
//-------------------------------------------------------------------------

type PackageFileCache struct {
	name     string // file name
	mtime    int64
	defalias string

	scope  *Scope
	main   *Decl // package declaration
	others map[string]*Decl

	// map which has that kind of entries:
	// ast -> go/ast
	// parser -> go/parser
	// etc.
	// used for replacing "" package names in .a files
	pathToAlias map[string]string
}

func NewPackageFileCache(name string) *PackageFileCache {
	m := new(PackageFileCache)
	m.name = name
	m.mtime = 0
	m.defalias = ""
	return m
}

// Creates a cache that stays in cache forever. Useful for built-in packages.
func NewPackageFileCacheForever(name, defalias string) *PackageFileCache {
	m := new(PackageFileCache)
	m.name = name
	m.mtime = -1
	m.defalias = defalias
	return m
}

func (m *PackageFileCache) findFile() string {
	if fileExists(m.name) {
		return m.name
	}

	n := len(m.name)
	filename := m.name[:n-1] + "6"
	if fileExists(filename) {
		return filename
	}

	filename = m.name[:n-1] + "8"
	if fileExists(filename) {
		return filename
	}

	filename = m.name[:n-1] + "5"
	if fileExists(filename) {
		return filename
	}
	return m.name
}

func (m *PackageFileCache) updateCache() {
	if m.mtime == -1 {
		return
	}
	fname := m.findFile()
	stat, err := os.Stat(fname)
	if err != nil {
		return
	}

	if m.mtime != stat.Mtime_ns {
		m.mtime = stat.Mtime_ns

		data, err := ioutil.ReadFile(fname)
		if err != nil {
			return
		}
		m.processPackageData(string(data))
	}
}

func (m *PackageFileCache) processPackageData(s string) {
	m.scope = NewScope(nil)

	// Find first $$
	i := strings.Index(s, "\n$$")
	if i == -1 {
		panic("Can't find the import section in the archive file")
	}
	s = s[i+len("\n$$"):]

	// Skip to the beginning of the package statement
	i = strings.Index(s, "package")
	if i == -1 {
		panic("Wrong file")
	}
	s = s[i:]

	// Find second $$
	i = strings.Index(s, "\n$$")
	if i == -1 {
		panic("Can't find the end of the import section in the archive file")
	}
	s = s[:i] // leave only import section

	i = strings.Index(s, "\n")
	if i == -1 {
		panic("Wrong file")
	}

	m.defalias = strings.TrimSpace(s[len("package "):i])
	s = s[i+1:]

	m.pathToAlias = make(map[string]string)
	// hack, add ourselves to the package scope
	m.addPackageToScope(m.defalias, m.name)
	// and the built-in "unsafe" package to the pathToAlias map
	m.pathToAlias["unsafe"] = "unsafe"
	internalPackages := make(map[string]*bytes.Buffer)
	for {
		// for each line
		i := strings.Index(s, "\n")
		if i == -1 {
			break
		}
		decl := strings.TrimSpace(s[:i])
		if len(decl) == 0 {
			s = s[i+1:]
			continue
		}
		decl2, pkg := m.processExport(decl)
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
	m.others = make(map[string]*Decl)
	for key, value := range internalPackages {
		tmp := m.expandPackages(removeBadMethods(value.Bytes()))
		decls, err := parser.ParseDeclList(token.NewFileSet(), "", tmp)

		if err != nil {
			panic(fmt.Sprintf("failure in:\n%s\n%s\n", tmp, err.String()))
		} else {
			tmp = nil
			if key == m.name {
				// main package
				m.main = NewDecl(m.name, DECL_PACKAGE, nil)
				addAstDeclsToPackage(m.main, decls, m.scope)
			} else {
				// others
				m.others[key] = NewDecl(key, DECL_PACKAGE, nil)
				addAstDeclsToPackage(m.others[key], decls, m.scope)
			}
		}

	}
	m.pathToAlias = nil
	for key, value := range m.scope.entities {
		pkg, ok := m.others[value.Name]
		if !ok && value.Name == m.name {
			pkg = m.main
		}
		m.scope.replaceDecl(key, pkg)
	}
}

// feed one definition line from .a file here
// returns:
// 1. a go/parser parsable string representing one Go declaration
// 2. and a package name this declaration belongs to
func (m *PackageFileCache) processExport(s string) (string, string) {
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
		m.processImportStatement(s)
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
	s = strings.Replace(s, "?", "__", -1)

	// skip system functions (Init, etc.)
	i = strings.Index(s, "·")
	if i != -1 {
		return "", ""
	}

	if pkg == "" {
		pkg = m.name
	}

	return s, pkg
}

func (m *PackageFileCache) processImportStatement(s string) {
	var scan scanner.Scanner
	set := token.NewFileSet()
	scan.Init(set.AddFile("", set.Base(), len(s)), []byte(s), nil, 0)

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
	m.pathToAlias[path] = alias
	m.addPackageToScope(alias, path)
}

func (m *PackageFileCache) expandPackages(s []byte) []byte {
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
			alias, ok := m.pathToAlias[string(s[b+1:e])]
			if ok {
				out.Write(s[begin:b])
				out.Write([]byte(alias))
			} else {
				out.Write(s[begin:i])
			}
		} else {
			out.Write(s[begin:b])
			out.Write([]byte(m.defalias))
		}

	}
	panic("unreachable")
}

func (m *PackageFileCache) addPackageToScope(alias, realname string) {
	d := NewDecl(realname, DECL_PACKAGE, nil)
	m.scope.addDecl(alias, d)
}

func addAstDeclsToPackage(pkg *Decl, decls []ast.Decl, scope *Scope) {
	for _, decl := range decls {
		foreachDecl(decl, func(data *foreachDeclStruct) {
			class := astDeclClass(data.decl)
			for i, name := range data.names {
				typ, v, vi := data.typeValueIndex(i, DECL_FOREIGN)

				d := NewDecl2(name.Name, class, DECL_FOREIGN, typ, v, vi, scope)
				if d == nil {
					return
				}

				if !name.IsExported() && d.Class != DECL_TYPE {
					return
				}

				methodof := MethodOf(data.decl)
				if methodof != "" {
					decl := pkg.FindChild(methodof)
					if decl != nil {
						decl.AddChild(d)
					} else {
						decl = NewDecl(methodof, DECL_METHODS_STUB, scope)
						decl.AddChild(d)
						pkg.AddChild(decl)
					}
				} else {
					decl := pkg.FindChild(d.Name)
					if decl != nil {
						decl.ExpandOrReplace(d)
					} else {
						pkg.AddChild(d)
					}
				}
			}
		})
	}
}

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

	i += 2            // skip to a first symbol after dot
	s = s[:b] + s[i:] // strip package clause completely

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

		i += 2            // skip to a first symbol after dot
		s = s[:b] + s[i:] // strip package clause completely

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

	return s[:b] + "0" + s[e:]
}

var badMethodsRE = regexp.MustCompile(`"[^"]*"\.[A-Za-z]+\(`)

func removePackagePart(data []byte) []byte {
	i := bytes.LastIndex(data, []byte(`".`))
	if i == -1 {
		panic("shouldn't be -1")
	}

	return data[i+2:]
}

func removeBadMethods(data []byte) []byte {
	return badMethodsRE.ReplaceAllFunc(data, removePackagePart)
}

//-------------------------------------------------------------------------
// PackageCache
//-------------------------------------------------------------------------

type PackageCache map[string]*PackageFileCache

func NewPackageCache() PackageCache {
	m := make(PackageCache)

	// add built-in "unsafe" package
	m.addBuiltinUnsafePackage()

	return m
}

// Function fills 'ps' set with packages from 'packages' import information.
// In case if package is not in the cache, it creates one and adds one to the cache.
func (c PackageCache) AppendPackages(ps map[string]*PackageFileCache, pkgs PackageImports) {
	for _, m := range pkgs {
		if _, ok := ps[m.Path]; ok {
			continue
		}

		if mod, ok := c[m.Path]; ok {
			ps[m.Path] = mod
		} else {
			mod = NewPackageFileCache(m.Path)
			ps[m.Path] = mod
			c[m.Path] = mod
		}
	}
}

const builtinUnsafePackage = `
import
$$
package unsafe 
	type "".Pointer *any
	func "".Offsetof (? any) int
	func "".Sizeof (? any) int
	func "".Alignof (? any) int
	func "".Typeof (i interface { }) interface { }
	func "".Reflect (i interface { }) (typ interface { }, addr "".Pointer)
	func "".Unreflect (typ interface { }, addr "".Pointer) interface { }
	func "".New (typ interface { }) "".Pointer
	func "".NewArray (typ interface { }, n int) "".Pointer

$$
`

func (c PackageCache) addBuiltinUnsafePackage() {
	name, _ := findGlobalFile("unsafe")
	pkg := NewPackageFileCacheForever(name, "unsafe")
	pkg.processPackageData(builtinUnsafePackage)
	c[name] = pkg
}
