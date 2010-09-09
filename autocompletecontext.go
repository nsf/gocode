package main

import (
	"fmt"
	"bytes"
	"go/parser"
	"go/ast"
	"io/ioutil"
	"strings"
	"path"
	"sort"
	"time"
	"container/vector"
	"runtime"
)

//-------------------------------------------------------------------------
// OutBuffers
// Temporary structure for writing autocomplete response.
//-------------------------------------------------------------------------

type OutBuffers struct {
	tmpbuf  *bytes.Buffer
	names   vector.StringVector
	types   vector.StringVector
	classes vector.StringVector
	ctx     *AutoCompleteContext
	tmpns   map[string]bool
}

func NewOutBuffers(ctx *AutoCompleteContext) *OutBuffers {
	b := new(OutBuffers)
	b.tmpbuf = bytes.NewBuffer(make([]byte, 0, 1024))
	b.names = vector.StringVector(make([]string, 0, 1024))
	b.types = vector.StringVector(make([]string, 0, 1024))
	b.classes = vector.StringVector(make([]string, 0, 1024))
	b.ctx = ctx
	return b
}

func (b *OutBuffers) Len() int {
	return b.names.Len()
}

func (b *OutBuffers) Less(i, j int) bool {
	if b.classes[i][0] == b.classes[j][0] {
		return b.names[i] < b.names[j]
	}
	return b.classes[i] < b.classes[j]
}

func (b *OutBuffers) Swap(i, j int) {
	b.names[i], b.names[j] = b.names[j], b.names[i]
	b.types[i], b.types[j] = b.types[j], b.types[i]
	b.classes[i], b.classes[j] = b.classes[j], b.classes[i]
}

func (b *OutBuffers) appendDecl(p, name string, decl *Decl, class int) {
	c1 := !Config.ProposeBuiltins && decl.Scope == universeScope
	c2 := class != -1 && !matchClass(int(decl.Class), class)
	c3 := class == -1 && !strings.HasPrefix(name, p)
	c4 := !decl.Matches()
	c5 := !checkTypeExpr(decl.Type)

	if c1 || c2 || c3 || c4 || c5 {
		return
	}

	b.names.Push(name)

	decl.PrettyPrintType(b.tmpbuf)
	b.types.Push(b.tmpbuf.String())
	b.tmpbuf.Reset()

	b.classes.Push(decl.ClassName())
}

func (b *OutBuffers) appendEmbedded(p string, decl *Decl, class int) {
	if decl.Embedded == nil {
		return
	}

	firstLevel := false
	if b.tmpns == nil {
		// first level, create tmp namespace
		b.tmpns = make(map[string]bool)
		firstLevel = true

		// add all children of the current decl to the namespace
		for _, c := range decl.Children {
			b.tmpns[c.Name] = true
		}
	}

	for _, emb := range decl.Embedded {
		typedecl := typeToDecl(emb, decl.Scope)
		if typedecl != nil {
			for _, c := range typedecl.Children {
				if _, has := b.tmpns[c.Name]; has {
					continue
				}
				b.appendDecl(p, c.Name, c, class)
				b.tmpns[c.Name] = true
			}
			b.appendEmbedded(p, typedecl, class)
		}
	}

	if firstLevel {
		// remove tmp namespace
		b.tmpns = nil
	}
}

func matchClass(declclass int, class int) bool {
	if class == declclass {
		return true
	}
	return false
}

//-------------------------------------------------------------------------
// AutoCompleteContext
// Context that holds cache structures for autocompletion needs. It
// includes cache for packages and for main package files.
//-------------------------------------------------------------------------

type AutoCompleteContext struct {
	current *AutoCompleteFile // currently editted file
	others  []*DeclFileCache  // other files of the current package
	pkg     *Scope

	pcache    PackageCache // packages cache
	declcache *DeclCache   // top-level declarations cache
}

func NewAutoCompleteContext(pcache PackageCache, declcache *DeclCache) *AutoCompleteContext {
	c := new(AutoCompleteContext)
	c.current = NewPackageFile("")
	c.pcache = pcache
	c.declcache = declcache
	return c
}

func (c *AutoCompleteContext) updateCaches() {
	// temporary map for packages that we need to check for a cache expiration
	// map is used as a set of unique items to prevent double checks
	ps := make(map[string]*PackageFileCache)

	// collect import information from all of the files
	c.pcache.AppendPackages(ps, c.current.packages)
	c.others = getOtherPackageFiles(c.current.name, c.current.packageName, c.declcache)
	for _, other := range c.others {
		c.pcache.AppendPackages(ps, other.Packages)
	}

	updatePackages(ps)

	// fix imports for all files
	fixupPackages(c.current.filescope, c.current.packages, c.pcache)
	for _, f := range c.others {
		fixupPackages(f.FileScope, f.Packages, c.pcache)
	}

	// At this point we have collected all top level declarations, now we need to
	// merge them in the common package block.
	c.mergeDecls()
}

func (c *AutoCompleteContext) mergeDecls() {
	c.pkg = NewScope(universeScope)
	mergeDecls(c.current.filescope, c.pkg, c.current.decls)
	for _, f := range c.others {
		mergeDecls(f.FileScope, c.pkg, f.Decls)
	}
}

func (c *AutoCompleteContext) makeDeclSet(scope *Scope) map[string]*Decl {
	set := make(map[string]*Decl, len(c.pkg.entities)*2)
	makeDeclSetRecursive(set, scope)
	return set
}

// returns three slices of the same length containing:
// 1. apropos names
// 2. apropos types (pretty-printed)
// 3. apropos classes
// and length of the part that should be replaced (if any)
func (c *AutoCompleteContext) Apropos(file []byte, filename string, cursor int) ([]string, []string, []string, int) {
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

	b := NewOutBuffers(c)

	partial := 0
	da := c.deduceDecl(file, cursor)
	if da != nil {
		class := -1
		switch da.Partial {
		case "const":
			class = DECL_CONST
		case "var":
			class = DECL_VAR
		case "type":
			class = DECL_TYPE
		case "func":
			class = DECL_FUNC
		case "package":
			class = DECL_PACKAGE
		}
		if da.Decl == nil {
			// In case if no declaraion is a subject of completion, propose all:
			set := c.makeDeclSet(c.current.scope)
			for key, value := range set {
				if value == nil {
					continue
				}
				value.InferType()
				b.appendDecl(da.Partial, key, value, class)
			}
		} else {
			// propose all children of a subject declaration and
			// propose all children of its embedded types
			for _, decl := range da.Decl.Children {
				if da.Decl.Class == DECL_PACKAGE && !ast.IsExported(decl.Name) {
					continue
				}
				b.appendDecl(da.Partial, decl.Name, decl, class)
			}
			b.appendEmbedded(da.Partial, da.Decl, class)
		}
		partial = len(da.Partial)
	}

	if b.names.Len() == 0 || b.types.Len() == 0 || b.classes.Len() == 0 {
		return nil, nil, nil, 0
	}

	sort.Sort(b)
	return b.names, b.types, b.classes, partial
}

func updatePackages(ps map[string]*PackageFileCache) {
	// initiate package cache update
	done := make(chan bool)
	for _, p := range ps {
		go func(p *PackageFileCache) {
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

func mergeDecls(filescope *Scope, pkg *Scope, decls map[string]*Decl) {
	for _, d := range decls {
		pkg.mergeDecl(d)
	}
	filescope.parent = pkg
}

func fixupPackages(filescope *Scope, pkgs PackageImports, pcache PackageCache) {
	for _, m := range pkgs {
		path, alias := m.Path, m.Alias
		if alias == "" {
			alias = pcache[path].defalias
		}
		filescope.replaceDecl(alias, pcache[path].main)
	}
}

func getOtherPackageFiles(filename, packageName string, declcache *DeclCache) []*DeclFileCache {
	others := findOtherPackageFiles(filename, packageName)

	ret := make([]*DeclFileCache, len(others))
	done := make(chan *DeclFileCache)

	for _, nm := range others {
		go func(name string) {
			defer func() {
				if err := recover(); err != nil {
					printBacktrace(err)
					done <- nil
				}
			}()
			done <- declcache.Get(name)
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

	dir, file := path.Split(filename)
	filesInDir, err := ioutil.ReadDir(dir)
	if err != nil {
		panic(err.String())
	}

	count := 0
	for _, stat := range filesInDir {
		ok, _ := path.Match("*.go", stat.Name)
		if !ok || stat.Name == file {
			continue
		}
		count++
	}

	out := make([]string, 0, count)
	for _, stat := range filesInDir {
		ok, _ := path.Match("*.go", stat.Name)
		if !ok || stat.Name == file {
			continue
		}

		abspath := path.Join(dir, stat.Name)
		if filePackageName(abspath) == packageName {
			n := len(out)
			out = out[0 : n+1]
			out[n] = abspath
		}
	}

	return out
}

func filePackageName(filename string) string {
	file, _ := parser.ParseFile(filename, nil, parser.PackageClauseOnly)
	return file.Name.Name
}

func makeDeclSetRecursive(set map[string]*Decl, scope *Scope) {
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

type DeclSlice []*Decl

func (s DeclSlice) Less(i, j int) bool {
	if s[i].ClassName()[0] == s[j].ClassName()[0] {
		return s[i].Name < s[j].Name
	}
	return s[i].ClassName() < s[j].ClassName()
}
func (s DeclSlice) Len() int      { return len(s) }
func (s DeclSlice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

const (
	COLOR_red     = "\033[0;31m"
	COLOR_RED     = "\033[1;31m"
	COLOR_green   = "\033[0;32m"
	COLOR_GREEN   = "\033[1;32m"
	COLOR_yellow  = "\033[0;33m"
	COLOR_YELLOW  = "\033[1;33m"
	COLOR_blue    = "\033[0;34m"
	COLOR_BLUE    = "\033[1;34m"
	COLOR_magenta = "\033[0;35m"
	COLOR_MAGENTA = "\033[1;35m"
	COLOR_cyan    = "\033[0;36m"
	COLOR_CYAN    = "\033[1;36m"
	COLOR_white   = "\033[0;37m"
	COLOR_WHITE   = "\033[1;37m"
	NC            = "\033[0m"
)

var declClassToColor = [...]string{
	DECL_CONST:        COLOR_WHITE,
	DECL_VAR:          COLOR_magenta,
	DECL_TYPE:         COLOR_cyan,
	DECL_FUNC:         COLOR_green,
	DECL_PACKAGE:      COLOR_red,
	DECL_METHODS_STUB: COLOR_red,
}

var declClassToStringStatus = [...]string{
	DECL_CONST:        "  const",
	DECL_VAR:          "    var",
	DECL_TYPE:         "   type",
	DECL_FUNC:         "   func",
	DECL_PACKAGE:      "package",
	DECL_METHODS_STUB: "   stub",
}

func (c *AutoCompleteContext) Status() string {
	buf := bytes.NewBuffer(make([]byte, 0, 4096))
	fmt.Fprintf(buf, "Server's GOMAXPROCS == %d\n", runtime.GOMAXPROCS(0))
	fmt.Fprintf(buf, "\nPackage cache contains %d entries\n", len(c.pcache))
	fmt.Fprintf(buf, "\nListing these entries:\n")
	for _, mod := range c.pcache {
		fmt.Fprintf(buf, "\tname: %s (default alias: %s)\n", mod.name, mod.defalias)
		fmt.Fprintf(buf, "\timports %d declarations and %d packages\n", len(mod.main.Children), len(mod.others))
		if mod.mtime == -1 {
			fmt.Fprintf(buf, "\tthis package stays in cache forever (built-in package)\n")
		} else {
			mtime := time.SecondsToLocalTime(mod.mtime)
			fmt.Fprintf(buf, "\tlast modification time: %s\n", mtime.String())
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

		const STATUS_DECLS = "\t%s%s" + NC + " " + COLOR_yellow + "%s" + NC + "\n"
		const STATUS_DECLS_CHILDREN = "\t%s%s" + NC + " " + COLOR_yellow + "%s" + NC + " (%d)\n"
		var ds DeclSlice
		var i int

		fmt.Fprintf(buf, "\n%s:\n", c.current.name)
		ds = make(DeclSlice, len(c.current.decls))
		i = 0
		for _, d := range c.current.decls {
			ds[i] = d
			i++
		}
		sort.Sort(ds)
		for _, d := range ds {
			if len(d.Children) > 0 {
				fmt.Fprintf(buf, STATUS_DECLS_CHILDREN,
					declClassToColor[d.Class],
					declClassToStringStatus[d.Class],
					d.Name, len(d.Children))
			} else {
				fmt.Fprintf(buf, STATUS_DECLS,
					declClassToColor[d.Class],
					declClassToStringStatus[d.Class],
					d.Name)
			}
		}

		for _, f := range c.others {
			fmt.Fprintf(buf, "\n%s:\n", f.name)
			ds = make(DeclSlice, len(f.Decls))
			i = 0
			for _, d := range f.Decls {
				ds[i] = d
				i++
			}
			sort.Sort(ds)
			for _, d := range ds {
				if len(d.Children) > 0 {
					fmt.Fprintf(buf, STATUS_DECLS_CHILDREN,
						declClassToColor[d.Class],
						declClassToStringStatus[d.Class],
						d.Name, len(d.Children))
				} else {
					fmt.Fprintf(buf, STATUS_DECLS,
						declClassToColor[d.Class],
						declClassToStringStatus[d.Class],
						d.Name)
				}
			}
		}
	}
	return buf.String()
}
