package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

//-------------------------------------------------------------------------
// outBuffers
//
// Temporary structure for writing autocomplete response.
//-------------------------------------------------------------------------

// fields must be exported for RPC
type candidate struct {
	Name  string
	Type  string
	Class declClass
}

type outBuffers struct {
	tmpbuf     *bytes.Buffer
	candidates []candidate
	ctx        *autoCompleteContext
	tmpns      map[string]bool
	ignorecase bool
}

func newOutBuffers(ctx *autoCompleteContext) *outBuffers {
	b := new(outBuffers)
	b.tmpbuf = bytes.NewBuffer(make([]byte, 0, 1024))
	b.candidates = make([]candidate, 0, 64)
	b.ctx = ctx
	return b
}

func (b *outBuffers) Len() int {
	return len(b.candidates)
}

func (b *outBuffers) Less(i, j int) bool {
	x := b.candidates[i]
	y := b.candidates[j]
	if x.Class == y.Class {
		return x.Name < y.Name
	}
	return x.Class < y.Class
}

func (b *outBuffers) Swap(i, j int) {
	b.candidates[i], b.candidates[j] = b.candidates[j], b.candidates[i]
}

func (b *outBuffers) appendDecl(p, name string, decl *decl, class declClass) {
	c1 := !gConfig.ProposeBuiltins && decl.scope == gUniverseScope && decl.name != "Error"
	c2 := class != declInvalid && decl.class != class
	c3 := class == declInvalid && !hasPrefix(name, p, b.ignorecase)
	c4 := !decl.matches()
	c5 := !checkTypeExpr(decl.typ)

	if c1 || c2 || c3 || c4 || c5 {
		return
	}

	decl.prettyPrintType(b.tmpbuf)
	b.candidates = append(b.candidates, candidate{
		Name:  name,
		Type:  b.tmpbuf.String(),
		Class: decl.class,
	})
	b.tmpbuf.Reset()
}

func (b *outBuffers) appendEmbedded(p string, decl *decl, class declClass) {
	if decl.embedded == nil {
		return
	}

	firstLevel := false
	if b.tmpns == nil {
		// first level, create tmp namespace
		b.tmpns = make(map[string]bool)
		firstLevel = true

		// add all children of the current decl to the namespace
		for _, c := range decl.children {
			b.tmpns[c.name] = true
		}
	}

	for _, emb := range decl.embedded {
		typedecl := typeToDecl(emb, decl.scope)
		if typedecl == nil {
			continue
		}

		// prevent infinite recursion here
		if typedecl.flags&declVisited != 0 {
			continue
		}
		typedecl.flags |= declVisited
		defer typedecl.clearVisited()

		for _, c := range typedecl.children {
			if _, has := b.tmpns[c.name]; has {
				continue
			}
			b.appendDecl(p, c.name, c, class)
			b.tmpns[c.name] = true
		}
		b.appendEmbedded(p, typedecl, class)
	}

	if firstLevel {
		// remove tmp namespace
		b.tmpns = nil
	}
}

//-------------------------------------------------------------------------
// autoCompleteContext
//
// Context that holds cache structures for autocompletion needs. It
// includes cache for packages and for main package files.
//-------------------------------------------------------------------------

type autoCompleteContext struct {
	current *autoCompleteFile // currently editted file
	others  []*declFileCache  // other files of the current package
	pkg     *scope

	pcache    packageCache // packages cache
	declcache *declCache   // top-level declarations cache
}

func newAutoCompleteContext(pcache packageCache, declcache *declCache) *autoCompleteContext {
	c := new(autoCompleteContext)
	c.current = newAutoCompleteFile("", declcache.env)
	c.pcache = pcache
	c.declcache = declcache
	return c
}

func (c *autoCompleteContext) updateCaches() {
	// temporary map for packages that we need to check for a cache expiration
	// map is used as a set of unique items to prevent double checks
	ps := make(map[string]*packageFileCache)

	// collect import information from all of the files
	c.pcache.appendPackages(ps, c.current.packages)
	c.others = getOtherPackageFiles(c.current.name, c.current.packageName, c.declcache)
	for _, other := range c.others {
		c.pcache.appendPackages(ps, other.packages)
	}

	updatePackages(ps)

	// fix imports for all files
	fixupPackages(c.current.filescope, c.current.packages, c.pcache)
	for _, f := range c.others {
		fixupPackages(f.filescope, f.packages, c.pcache)
	}

	// At this point we have collected all top level declarations, now we need to
	// merge them in the common package block.
	c.mergeDecls()
}

func (c *autoCompleteContext) mergeDecls() {
	c.pkg = newScope(gUniverseScope)
	mergeDecls(c.current.filescope, c.pkg, c.current.decls)
	mergeDeclsFromPackages(c.pkg, c.current.packages, c.pcache)
	for _, f := range c.others {
		mergeDecls(f.filescope, c.pkg, f.decls)
		mergeDeclsFromPackages(c.pkg, f.packages, c.pcache)
	}
}

func (c *autoCompleteContext) makeDeclSet(scope *scope) map[string]*decl {
	set := make(map[string]*decl, len(c.pkg.entities)*2)
	makeDeclSetRecursive(set, scope)
	return set
}

func (c *autoCompleteContext) getCandidatesFromSet(set map[string]*decl, partial string, class declClass, b *outBuffers) {
	for key, value := range set {
		if value == nil {
			continue
		}
		value.inferType()
		b.appendDecl(partial, key, value, class)
	}
}

func (c *autoCompleteContext) getCandidatesFromDecl(cc cursorContext, class declClass, b *outBuffers) {
	// propose all children of a subject declaration and
	for _, decl := range cc.decl.children {
		if cc.decl.class == declPackage && !ast.IsExported(decl.name) {
			continue
		}
		b.appendDecl(cc.partial, decl.name, decl, class)
	}
	// propose all children of an underlying struct/interface type
	adecl := advanceToStructOrInterface(cc.decl)
	if adecl != nil && adecl != cc.decl {
		for _, decl := range adecl.children {
			if decl.class == declVar {
				b.appendDecl(cc.partial, decl.name, decl, class)
			}
		}
	}
	// propose all children of its embedded types
	b.appendEmbedded(cc.partial, cc.decl, class)
}

// returns three slices of the same length containing:
// 1. apropos names
// 2. apropos types (pretty-printed)
// 3. apropos classes
// and length of the part that should be replaced (if any)
func (c *autoCompleteContext) apropos(file []byte, filename string, cursor int) ([]candidate, int) {
	c.current.cursor = cursor
	c.current.name = filename

	// Update caches and parse the current file.
	// This process is quite complicated, because I was trying to design it in a
	// concurrent fashion. Apparently I'm not really good at that. Hopefully
	// will be better in future.

	// Does full processing of the currently editted file (top-level declarations plus
	// active function).
	c.current.processData(file)

	// Updates cache of other files and packages. See the function for details of
	// the process. At the end merges all the top-level declarations into the package
	// block.
	c.updateCaches()

	// And we're ready to Go. ;)

	b := newOutBuffers(c)

	partial := 0
	cc, ok := c.deduceCursorContext(file, cursor)
	if !ok {
		return nil, 0
	}

	class := declInvalid
	switch cc.partial {
	case "const":
		class = declConst
	case "var":
		class = declVar
	case "type":
		class = declType
	case "func":
		class = declFunc
	case "package":
		class = declPackage
	}

	if cc.decl == nil {
		// In case if no declaraion is a subject of completion, propose all:
		set := c.makeDeclSet(c.current.scope)
		c.getCandidatesFromSet(set, cc.partial, class, b)
		if cc.partial != "" && len(b.candidates) == 0 {
			// as a fallback, try case insensitive approach
			b.ignorecase = true
			c.getCandidatesFromSet(set, cc.partial, class, b)
		}
	} else {
		c.getCandidatesFromDecl(cc, class, b)
		if cc.partial != "" && len(b.candidates) == 0 {
			// as a fallback, try case insensitive approach
			b.ignorecase = true
			c.getCandidatesFromDecl(cc, class, b)
		}
	}
	partial = len(cc.partial)

	if len(b.candidates) == 0 {
		return nil, 0
	}

	sort.Sort(b)
	return b.candidates, partial
}

func (c *autoCompleteContext) cursorTypePkg(file []byte, filename string, cursor int) (string, string) {
	c.current.cursor = cursor
	c.current.name = filename
	c.current.processData(file)
	c.updateCaches()
	typ, pkg, ok := c.deduceCursorTypePkg(file, cursor)
	if !ok || typ == nil {
		return "", ""
	}

	var tmp bytes.Buffer
	prettyPrintTypeExpr(&tmp, typ)
	return tmp.String(), pkg
}

func updatePackages(ps map[string]*packageFileCache) {
	// initiate package cache update
	done := make(chan bool)
	for _, p := range ps {
		go func(p *packageFileCache) {
			defer func() {
				if err := recover(); err != nil {
					printBacktrace(err)
					done <- false
				}
			}()
			p.updateCache()
			done <- true
		}(p)
	}

	// wait for its completion
	for _ = range ps {
		if !<-done {
			panic("One of the package cache updaters panicked")
		}
	}
}

func mergeDecls(filescope *scope, pkg *scope, decls map[string]*decl) {
	for _, d := range decls {
		pkg.mergeDecl(d)
	}
	filescope.parent = pkg
}

func mergeDeclsFromPackages(pkgscope *scope, pkgs []packageImport, pcache packageCache) {
	for _, p := range pkgs {
		path, alias := p.path, p.alias
		if alias != "." {
			continue
		}
		p := pcache[path].main
		if p == nil {
			continue
		}
		for _, d := range p.children {
			if ast.IsExported(d.name) {
				pkgscope.mergeDecl(d)
			}
		}
	}
}

func fixupPackages(filescope *scope, pkgs []packageImport, pcache packageCache) {
	for _, p := range pkgs {
		path, alias := p.path, p.alias
		if alias == "" {
			alias = pcache[path].defalias
		}
		// skip packages that will be merged to the package scope
		if alias == "." {
			continue
		}
		filescope.replaceDecl(alias, pcache[path].main)
	}
}

func getOtherPackageFiles(filename, packageName string, declcache *declCache) []*declFileCache {
	others := findOtherPackageFiles(filename, packageName)

	ret := make([]*declFileCache, len(others))
	done := make(chan *declFileCache)

	for _, nm := range others {
		go func(name string) {
			defer func() {
				if err := recover(); err != nil {
					printBacktrace(err)
					done <- nil
				}
			}()
			done <- declcache.getAndUpdate(name)
		}(nm)
	}

	for i := range others {
		ret[i] = <-done
		if ret[i] == nil {
			panic("One of the decl cache updaters panicked")
		}
	}

	return ret
}

func findOtherPackageFiles(filename, packageName string) []string {
	if filename == "" {
		return nil
	}

	dir, file := filepath.Split(filename)
	filesInDir, err := ioutil.ReadDir(dir)
	if err != nil {
		panic(err)
	}

	count := 0
	for _, stat := range filesInDir {
		ok, _ := filepath.Match("*.go", stat.Name())
		if !ok || stat.Name() == file {
			continue
		}
		count++
	}

	out := make([]string, 0, count)
	for _, stat := range filesInDir {
		const nonRegular = os.ModeDir | os.ModeSymlink |
			os.ModeDevice | os.ModeNamedPipe | os.ModeSocket

		ok, _ := filepath.Match("*.go", stat.Name())
		if !ok || stat.Name() == file || stat.Mode()&nonRegular != 0 {
			continue
		}

		abspath := filepath.Join(dir, stat.Name())
		if filePackageName(abspath) == packageName {
			n := len(out)
			out = out[:n+1]
			out[n] = abspath
		}
	}

	return out
}

func filePackageName(filename string) string {
	file, _ := parser.ParseFile(token.NewFileSet(), filename, nil, parser.PackageClauseOnly)
	return file.Name.Name
}

func makeDeclSetRecursive(set map[string]*decl, scope *scope) {
	for name, ent := range scope.entities {
		if _, ok := set[name]; !ok {
			set[name] = ent
		}
	}
	if scope.parent != nil {
		makeDeclSetRecursive(set, scope.parent)
	}
}

func checkFuncFieldList(f *ast.FieldList) bool {
	if f == nil {
		return true
	}

	for _, field := range f.List {
		if !checkTypeExpr(field.Type) {
			return false
		}
	}
	return true
}

// checks for a type expression correctness, it the type expression has
// ast.BadExpr somewhere, returns false, otherwise true
func checkTypeExpr(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.StarExpr:
		return checkTypeExpr(t.X)
	case *ast.ArrayType:
		return checkTypeExpr(t.Elt)
	case *ast.SelectorExpr:
		return checkTypeExpr(t.X)
	case *ast.FuncType:
		a := checkFuncFieldList(t.Params)
		b := checkFuncFieldList(t.Results)
		return a && b
	case *ast.MapType:
		a := checkTypeExpr(t.Key)
		b := checkTypeExpr(t.Value)
		return a && b
	case *ast.Ellipsis:
		return checkTypeExpr(t.Elt)
	case *ast.ChanType:
		return checkTypeExpr(t.Value)
	case *ast.BadExpr:
		return false
	default:
		return true
	}
	return true
}

//-------------------------------------------------------------------------
// Status output
//-------------------------------------------------------------------------

type declSlice []*decl

func (s declSlice) Less(i, j int) bool {
	if s[i].class != s[j].class {
		return s[i].name < s[j].name
	}
	return s[i].class < s[j].class
}
func (s declSlice) Len() int      { return len(s) }
func (s declSlice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

const (
	colorRed          = "\033[0;31m"
	colorRedBold     = "\033[1;31m"
	colorGreen        = "\033[0;32m"
	colorGreenBold   = "\033[1;32m"
	colorYellow       = "\033[0;33m"
	colorYellowBold  = "\033[1;33m"
	colorBlue         = "\033[0;34m"
	colorBlueBold    = "\033[1;34m"
	colorMagenta      = "\033[0;35m"
	colorMagentaBold = "\033[1;35m"
	colorCyan         = "\033[0;36m"
	colorCyanBold    = "\033[1;36m"
	colorWhite        = "\033[0;37m"
	colorWhiteBold   = "\033[1;37m"
	colorNone         = "\033[0m"
)

var gDeclClassToColor = [...]string{
	declConst:        colorWhiteBold,
	declVar:          colorMagenta,
	declType:         colorCyan,
	declFunc:         colorGreen,
	declPackage:      colorRed,
	declMethodsStub: colorRed,
}

var gDeclClassToStringStatus = [...]string{
	declConst:        "  const",
	declVar:          "    var",
	declType:         "   type",
	declFunc:         "   func",
	declPackage:      "package",
	declMethodsStub: "   stub",
}

func (c *autoCompleteContext) status() string {

	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	fmt.Fprintf(buf, "Server's GOMAXPROCS == %d\n", runtime.GOMAXPROCS(0))
	fmt.Fprintf(buf, "\nPackage cache contains %d entries\n", len(c.pcache))
	fmt.Fprintf(buf, "\nListing these entries:\n")
	for _, mod := range c.pcache {
		fmt.Fprintf(buf, "\tname: %s (default alias: %s)\n", mod.name, mod.defalias)
		fmt.Fprintf(buf, "\timports %d declarations and %d packages\n", len(mod.main.children), len(mod.others))
		if mod.mtime == -1 {
			fmt.Fprintf(buf, "\tthis package stays in cache forever (built-in package)\n")
		} else {
			mtime := time.Unix(0, mod.mtime)
			fmt.Fprintf(buf, "\tlast modification time: %s\n", mtime)
		}
		fmt.Fprintf(buf, "\n")
	}
	if c.current.name != "" {
		fmt.Fprintf(buf, "Last editted file: %s (package: %s)\n", c.current.name, c.current.packageName)
		if len(c.others) > 0 {
			fmt.Fprintf(buf, "\nOther files from the current package:\n")
		}
		for _, f := range c.others {
			fmt.Fprintf(buf, "\t%s\n", f.name)
		}
		fmt.Fprintf(buf, "\nListing declarations from files:\n")

		const statusDecls = "\t%s%s" + colorNone + " " + colorYellow + "%s" + colorNone + "\n"
		const statusDeclsChildren = "\t%s%s" + colorNone + " " + colorYellow + "%s" + colorNone + " (%d)\n"

		fmt.Fprintf(buf, "\n%s:\n", c.current.name)
		ds := make(declSlice, len(c.current.decls))
		i := 0
		for _, d := range c.current.decls {
			ds[i] = d
			i++
		}
		sort.Sort(ds)
		for _, d := range ds {
			if len(d.children) > 0 {
				fmt.Fprintf(buf, statusDeclsChildren,
					gDeclClassToColor[d.class],
					gDeclClassToStringStatus[d.class],
					d.name, len(d.children))
			} else {
				fmt.Fprintf(buf, statusDecls,
					gDeclClassToColor[d.class],
					gDeclClassToStringStatus[d.class],
					d.name)
			}
		}

		for _, f := range c.others {
			fmt.Fprintf(buf, "\n%s:\n", f.name)
			ds = make(declSlice, len(f.decls))
			i = 0
			for _, d := range f.decls {
				ds[i] = d
				i++
			}
			sort.Sort(ds)
			for _, d := range ds {
				if len(d.children) > 0 {
					fmt.Fprintf(buf, statusDeclsChildren,
						gDeclClassToColor[d.class],
						gDeclClassToStringStatus[d.class],
						d.name, len(d.children))
				} else {
					fmt.Fprintf(buf, statusDecls,
						gDeclClassToColor[d.class],
						gDeclClassToStringStatus[d.class],
						d.name)
				}
			}
		}
	}
	return buf.String()
}
