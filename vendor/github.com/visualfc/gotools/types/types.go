// Copyright 2011-2018 visualfc <visualfc@gmail.com>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package types

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/visualfc/fastmod"
	"github.com/visualfc/gotools/pkg/buildctx"
	"github.com/visualfc/gotools/pkg/command"
	"github.com/visualfc/gotools/pkg/pkgutil"
	"github.com/visualfc/gotools/pkg/stdlib"
	"golang.org/x/tools/go/buildutil"
)

var Command = &command.Command{
	Run:       runTypes,
	UsageLine: "types",
	Short:     "golang type util",
	Long:      `golang type util`,
}

var (
	typesVerbose        bool
	typesAllowBinary    bool
	typesFilePos        string
	typesCursorText     string
	typesFileStdin      bool
	typesFindUse        bool
	typesFindDef        bool
	typesFindUseAll     bool
	typesFindSkipGoroot bool
	typesFindInfo       bool
	typesFindDoc        bool
	typesTags           string
	typesTagList        = []string{} // exploded version of tags flag; set in main
)

//func init
func init() {
	Command.Flag.BoolVar(&typesVerbose, "v", false, "verbose debugging")
	Command.Flag.BoolVar(&typesAllowBinary, "b", false, "import can be satisfied by a compiled package object without corresponding sources.")
	Command.Flag.StringVar(&typesFilePos, "pos", "", "file position \"file.go:pos\"")
	Command.Flag.StringVar(&typesCursorText, "text", "", "file cursor text info")
	Command.Flag.BoolVar(&typesFileStdin, "stdin", false, "input file use stdin")
	Command.Flag.BoolVar(&typesFindInfo, "info", false, "find cursor info")
	Command.Flag.BoolVar(&typesFindDef, "def", false, "find cursor define")
	Command.Flag.BoolVar(&typesFindUse, "use", false, "find cursor usages")
	Command.Flag.BoolVar(&typesFindUseAll, "all", false, "find cursor all usages in GOPATH")
	Command.Flag.BoolVar(&typesFindSkipGoroot, "skip_goroot", false, "find cursor all usages skip GOROOT")
	Command.Flag.BoolVar(&typesFindDoc, "doc", false, "find cursor def doc")
	Command.Flag.StringVar(&typesTags, "tags", "", "space-separated list of build tags to apply when parsing")
}

type ObjKind int

const (
	ObjNone ObjKind = iota
	ObjPkgName
	ObjTypeName
	ObjInterface
	ObjStruct
	ObjConst
	ObjVar
	ObjField
	ObjFunc
	ObjMethod
	ObjLabel
	ObjBuiltin
	ObjNil
	ObjImplicit
	ObjUnknown
	ObjComment
)

var ObjKindName = []string{"none", "package",
	"type", "interface", "struct",
	"const", "var", "field",
	"func", "method",
	"label", "builtin", "nil",
	"implicit", "unknown", "comment"}

func (k ObjKind) String() string {
	if k >= 0 && int(k) < len(ObjKindName) {
		return ObjKindName[k]
	}
	return "unkwnown"
}

var builtinInfoMap = map[string]string{
	"append":   "func append(slice []Type, elems ...Type) []Type",
	"copy":     "func copy(dst, src []Type) int",
	"delete":   "func delete(m map[Type]Type1, key Type)",
	"len":      "func len(v Type) int",
	"cap":      "func cap(v Type) int",
	"make":     "func make(Type, size IntegerType) Type",
	"new":      "func new(Type) *Type",
	"complex":  "func complex(r, i FloatType) ComplexType",
	"real":     "func real(c ComplexType) FloatType",
	"imag":     "func imag(c ComplexType) FloatType",
	"close":    "func close(c chan<- Type)",
	"panic":    "func panic(v interface{})",
	"recover":  "func recover() interface{}",
	"print":    "func print(args ...Type)",
	"println":  "func println(args ...Type)",
	"error":    "type error interface {Error() string}",
	"Sizeof":   "func unsafe.Sizeof(any) uintptr",
	"Offsetof": "func unsafe.Offsetof(any) uintptr",
	"Alignof":  "func unsafe.Alignof(any) uintptr",
}

func builtinInfo(id string) string {
	if info, ok := builtinInfoMap[id]; ok {
		return "builtin " + info
	}
	return "builtin " + id
}

func simpleObjInfo(obj types.Object) string {
	s := obj.String()
	pkg := obj.Pkg()
	if pkg != nil {
		s = strings.Replace(s, pkg.Path(), pkg.Name(), -1)
		s = simpleType(s)
		if pkg.Name() == "main" {
			s = strings.Replace(s, "main.", "", -1)
		}
	}
	return s
}

func simpleType(src string) string {
	re, _ := regexp.Compile("[\\w\\./]+")
	return re.ReplaceAllStringFunc(src, func(s string) string {
		r := s
		if i := strings.LastIndex(s, "/"); i != -1 {
			r = s[i+1:]
		}
		if strings.Count(r, ".") > 1 {
			r = r[strings.Index(r, ".")+1:]
		}
		return r
	})
}

func runTypes(cmd *command.Command, args []string) error {
	if len(args) < 1 {
		cmd.Usage()
		return nil
	}
	if typesVerbose {
		now := time.Now()
		defer func() {
			cmd.Println("time", time.Now().Sub(now))
		}()
	}
	typesTagList = strings.Split(typesTags, " ")
	context := buildctx.System()
	context.BuildTags = append(typesTagList, context.BuildTags...)

	w := NewPkgWalker(context)
	cursor := &FileCursor{}
	cursor.text = typesCursorText
	if typesFilePos != "" {
		pos := strings.Index(typesFilePos, ":")
		if pos != -1 {
			cursor.fileName = typesFilePos[:pos]
			if i, err := strconv.Atoi(typesFilePos[pos+1:]); err == nil {
				cursor.cursorPos = i
			}
		}
		if typesFileStdin {
			src, err := ioutil.ReadAll(cmd.Stdin)
			if err == nil {
				cursor.src = src
			}
		}
	}
	w.cmd = cmd
	w.findMode = &FindMode{
		Info:       typesFindInfo,
		Define:     typesFindDef,
		Doc:        typesFindDoc,
		Usage:      typesFindUse,
		UsageAll:   typesFindUseAll,
		SkipGoroot: typesFindSkipGoroot,
	}

	for _, pkgName := range args {
		if pkgName == "." {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			pkgName = dir
			cursor.fileDir = dir
		}
		if cursor.src != nil {
			w.UpdateSourceData(filepath.Join(pkgName, cursor.fileName), cursor.src, false)
		}
		conf := DefaultPkgConfig()
		pkg, outconf, err := w.Check(pkgName, conf, cursor)
		if pkg == nil {
			return fmt.Errorf("error import path %v", err)
		}
		if cursor != nil && w.findMode.IsValid() {
			return w.LookupCursor(pkg, outconf, cursor)
		}
	}
	return nil
}

type FileCursor struct {
	fileName  string
	fileDir   string
	cursorPos int
	pos       token.Pos
	src       []byte
	xtest     bool
	text      string
}

func NewFileCursor(src []byte, dir string, filename string, pos int) *FileCursor {
	return &FileCursor{fileDir: dir, fileName: filename, cursorPos: pos, src: src}
}

func (f *FileCursor) SetText(text string) {
	f.text = text
}

type SourceData struct {
	data  []byte
	mtime int64
}

type FindMode struct {
	Info       bool
	Doc        bool
	Define     bool
	Usage      bool
	UsageAll   bool
	SkipGoroot bool
}

func (f *FindMode) IsValid() bool {
	return f.Info || f.Define || f.Usage
}

type PkgConfig struct {
	Pkg              *types.Package
	XPkg             *types.Package
	Info             *types.Info
	XInfo            *types.Info
	Bpkg             *build.Package
	Files            map[string]*ast.File
	XTestFiles       map[string]*ast.File
	IgnoreFuncBodies bool
	AllowBinary      bool
	WithTestFiles    bool
}

func DefaultPkgConfig() *PkgConfig {
	conf := &PkgConfig{IgnoreFuncBodies: false, AllowBinary: true, WithTestFiles: true}
	conf.Info = &types.Info{
		Uses:       make(map[*ast.Ident]types.Object),
		Defs:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Scopes:     make(map[ast.Node]*types.Scope),
		Implicits:  make(map[ast.Node]types.Object),
	}
	conf.XInfo = &types.Info{
		Uses:       make(map[*ast.Ident]types.Object),
		Defs:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Scopes:     make(map[ast.Node]*types.Scope),
		Implicits:  make(map[ast.Node]types.Object),
	}
	return conf
}

func NewPkgConfig(ignoreFuncBodies bool, withTestFiles bool) *PkgConfig {
	conf := &PkgConfig{IgnoreFuncBodies: ignoreFuncBodies, AllowBinary: true, WithTestFiles: withTestFiles}
	conf.Info = &types.Info{
		Uses:       make(map[*ast.Ident]types.Object),
		Defs:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Scopes:     make(map[ast.Node]*types.Scope),
		Implicits:  make(map[ast.Node]types.Object),
	}
	conf.XInfo = &types.Info{
		Uses:       make(map[*ast.Ident]types.Object),
		Defs:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Scopes:     make(map[ast.Node]*types.Scope),
		Implicits:  make(map[ast.Node]types.Object),
	}
	return conf
}

type PkgWalker struct {
	FileSet            *token.FileSet
	Context            *build.Context
	current            *types.Package
	importingName      map[string]bool
	ParsedFileCache    map[string]*ast.File
	ParsedFileModTime  map[string]int64
	fileSourceData     map[string]*SourceData
	Imported           map[string]*types.Package // packages already imported
	ImportedConfig     map[string]*PkgConfig
	ImportedFilesCheck map[string]*FilesCheck
	gcimported         types.Importer
	cmd                *command.Command
	ModPkg             *fastmod.Package
	findMode           *FindMode
}

func NewPkgWalker(context *build.Context) *PkgWalker {
	return &PkgWalker{
		Context:            context,
		FileSet:            token.NewFileSet(),
		ParsedFileCache:    map[string]*ast.File{},
		ParsedFileModTime:  map[string]int64{},
		fileSourceData:     map[string]*SourceData{},
		importingName:      map[string]bool{},
		Imported:           map[string]*types.Package{"unsafe": types.Unsafe},
		ImportedConfig:     map[string]*PkgConfig{},
		ImportedFilesCheck: map[string]*FilesCheck{},
		gcimported:         importer.Default(),
		findMode:           &FindMode{},
	}
}

func (w *PkgWalker) SetOutput(stdout io.Writer, stderr io.Writer) {
	cmd := &command.Command{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	w.cmd = cmd
}

func (w *PkgWalker) SetFindMode(mode *FindMode) {
	w.findMode = mode
}

func (w *PkgWalker) UpdateSourceData(filename string, data []byte, cleanAllSourceCache bool) {
	if cleanAllSourceCache {
		w.fileSourceData = make(map[string]*SourceData)
		delete(w.ParsedFileModTime, filename)
	}
	if sd, ok := w.fileSourceData[filename]; ok {
		if bytes.Equal(sd.data, data) {
			return
		}
	}
	w.fileSourceData[filename] = &SourceData{data, time.Now().UnixNano()}
}

func (p *PkgWalker) Check(name string, conf *PkgConfig, cusror *FileCursor) (pkg *types.Package, outconf *PkgConfig, err error) {
	if name == "." {
		name, _ = os.Getwd()
	}
	if conf == nil {
		conf = DefaultPkgConfig()
	}
	//p.Imported[name] = nil
	p.importingName = make(map[string]bool)
	p.ModPkg = nil
	// check mod
	var import_path string
	if filepath.IsAbs(name) {
		p.ModPkg, _ = fastmod.LoadPackage(name, p.Context)
		if p.ModPkg != nil {
			dir := filepath.ToSlash(p.ModPkg.Node().ModDir())
			fname := filepath.ToSlash(name)
			if dir == fname {
				import_path = p.ModPkg.Node().Path()
			} else if strings.HasPrefix(fname, dir+"/") {
				import_path = p.ModPkg.Node().Path() + fname[len(dir):]
			}
		}
	}
	pkg, outconf, err = p.ImportHelper("", name, import_path, conf, cusror)
	return
}

func contains(list []string, s string) bool {
	for _, t := range list {
		if t == s {
			return true
		}
	}
	return false
}

func (w *PkgWalker) isBinaryPkg(pkg string) bool {
	return stdlib.IsStdPkg(pkg)
}

func (w *PkgWalker) importPath(parentDir string, path string, mode build.ImportMode) (*build.Package, error) {
	if filepath.IsAbs(path) {
		return w.Context.ImportDir(path, 0)
	}
	if stdlib.IsStdPkg(path) {
		return stdlib.ImportStdPkg(w.Context, path, build.AllowBinary)
	}
	if w.ModPkg != nil {
		_path, dir, _ := w.ModPkg.Lookup(path)
		if dir != "" {
			pkg, err := w.Context.ImportDir(dir, mode)
			if pkg != nil {
				pkg.ImportPath = _path
			}
			return pkg, err
		}
	}
	if path == "syscall/js" {
		ctx := *w.Context
		ctx.BuildTags = append(ctx.BuildTags, "js")
		ctx.BuildTags = append(ctx.BuildTags, "wasm")
		return ctx.Import(path, "", mode)
	}
	return w.Context.Import(path, "", mode)
}

func (w *PkgWalker) Import(parentDir string, name string, conf *PkgConfig, cursor *FileCursor) (pkg *types.Package, outconf *PkgConfig, err error) {
	return w.ImportHelper(parentDir, name, "", conf, cursor)
}

type FilesCheck struct {
	HashSum [16]byte
	ModTime int64
}

func (w *PkgWalker) checkFiles(dir string, files []string) *FilesCheck {
	chk := &FilesCheck{}
	sort.Strings(files)
	var temp string
	for _, file := range files {
		filename := filepath.Join(dir, file)
		info, err := os.Lstat(filename)
		if err != nil {
			continue
		}
		t := info.ModTime().UnixNano()
		if sd, ok := w.fileSourceData[filename]; ok {
			if sd.mtime > t {
				t = sd.mtime
			} else {
				delete(w.fileSourceData, filename)
			}
		}
		temp += file
		if chk.ModTime < t {
			chk.ModTime = t
		}
	}
	chk.HashSum = md5.Sum([]byte(temp))
	return chk
}

func (w *PkgWalker) ImportHelper(parentDir string, name string, import_path string, conf *PkgConfig, cursor *FileCursor) (pkg *types.Package, outconf *PkgConfig, err error) {
	defer func() {
		err := recover()
		if err != nil && typesVerbose {
			fmt.Println(w.cmd.Stderr, err)
		}
	}()

	if parentDir != "" {
		if strings.HasPrefix(name, ".") {
			name = filepath.Join(parentDir, name)
		} else {
			if pkgutil.IsVendorExperiment() {
				parentPkg := pkgutil.ImportDirEx(w.Context, parentDir)
				var err error
				name, err = pkgutil.VendoredImportPath(parentPkg, name)
				if err != nil {
					return nil, nil, err
				}
			}
		}
	}

	bp, err := w.importPath(parentDir, name, 0)

	if bp == nil {
		return nil, nil, err
	}

	GoFiles := append(append([]string{}, bp.GoFiles...), bp.CgoFiles...)
	if conf.WithTestFiles {
		GoFiles = append(GoFiles, bp.TestGoFiles...)
	}
	conf.Bpkg = bp
	XTestGoFiles := append([]string{}, bp.XTestGoFiles...)

	//check cursor file
	if cursor != nil && cursor.fileName != "" {
		f, _ := w.parseFile(bp.Dir, cursor.fileName)
		if f != nil {
			cursor.pos = token.Pos(w.FileSet.File(f.Pos()).Base()) + token.Pos(cursor.cursorPos)
			cursor.fileDir = bp.Dir
			isTest := strings.HasSuffix(cursor.fileName, "_test.go")
			isXTest := false
			if isTest && strings.HasSuffix(f.Name.Name, "_test") {
				isXTest = true
			}
			cursor.xtest = isXTest
			checkInsert := func(filenames []string, file string) []string {
				for _, f := range filenames {
					if f == file {
						return filenames
					}
				}
				return append([]string{file}, filenames...)
			}
			if isXTest {
				XTestGoFiles = checkInsert(XTestGoFiles, cursor.fileName)
			} else {
				GoFiles = checkInsert(GoFiles, cursor.fileName)
			}
		}
	}

	pkg = w.Imported[name]
	chkFiles := w.checkFiles(bp.Dir, append(append([]string{}, GoFiles...), XTestGoFiles...))
	if pkg != nil {
		if chk, ok := w.ImportedFilesCheck[name]; ok {
			if chkFiles.ModTime == chk.ModTime && bytes.Equal(chkFiles.HashSum[:], chk.HashSum[:]) {
				outconf := w.ImportedConfig[name]
				if outconf != nil {
					var errcheck bool
					if !conf.IgnoreFuncBodies && outconf.IgnoreFuncBodies {
						errcheck = true
					} else if conf.WithTestFiles && !outconf.WithTestFiles {
						errcheck = true
					}
					if !errcheck {
						return pkg, outconf, nil
					}
				}
			}
		}
	}

	if typesVerbose {
		w.cmd.Println("parser pkg", parentDir, name)
	}
	checkName := name

	if bp.ImportPath == "." {
		checkName = bp.Name
	} else {
		checkName = bp.ImportPath
	}

	if import_path != "" {
		checkName = import_path
	}

	if w.importingName[checkName] {
		return nil, nil, fmt.Errorf("cycle importing package %q", name)
	}

	w.importingName[checkName] = true

	parserFiles := func(filenames []string, cursor *FileCursor, xtest bool) (files []*ast.File, fileMap map[string]*ast.File) {
		fileMap = make(map[string]*ast.File)
		for _, file := range filenames {
			var f *ast.File
			f, err = w.parseFile(bp.Dir, file)
			if cursor != nil && cursor.fileName == file {
				cursor.pos = token.Pos(w.FileSet.File(f.Pos()).Base()) + token.Pos(cursor.cursorPos)
				cursor.fileDir = bp.Dir
				cursor.xtest = xtest
			}
			if err != nil && typesVerbose {
				fmt.Fprintln(w.cmd.Stderr, err)
			}
			files = append(files, f)
			fileMap[file] = f
		}
		return
	}
	var files []*ast.File
	var xfiles []*ast.File
	files, conf.Files = parserFiles(GoFiles, cursor, false)
	xfiles, conf.XTestFiles = parserFiles(XTestGoFiles, cursor, true)

	typesConf := types.Config{
		IgnoreFuncBodies: conf.IgnoreFuncBodies,
		FakeImportC:      true,
		Importer:         &Importer{w, conf, bp.Dir},
		Error: func(err error) {
			if typesVerbose {
				fmt.Fprintln(w.cmd.Stderr, err)
			}
		},
	}

	pkg, err = typesConf.Check(checkName, w.FileSet, files, conf.Info)
	conf.Pkg = pkg

	w.importingName[checkName] = false
	w.Imported[name] = pkg
	w.ImportedConfig[name] = conf
	w.ImportedFilesCheck[name] = chkFiles
	outconf = conf

	if len(xfiles) > 0 {
		xpkg, _ := typesConf.Check(checkName+"_test", w.FileSet, xfiles, conf.XInfo)
		w.Imported[checkName+"_test"] = xpkg
		conf.XPkg = xpkg
	}
	return
}

type Importer struct {
	w    *PkgWalker
	conf *PkgConfig
	dir  string
}

func (im *Importer) Import(name string) (pkg *types.Package, err error) {
	if im.conf.AllowBinary && im.w.isBinaryPkg(name) {
		if found := im.w.Imported[name]; found != nil {
			return found, nil
		}
		pkg, err = im.w.gcimported.Import(name)
		if pkg != nil && pkg.Complete() {
			im.w.Imported[name] = pkg
			return
		}
		//		pkg = im.w.gcimporter[name]
		//		if pkg != nil && pkg.Complete() {
		//			return
		//		}
		//		pkg, err = importer.Default().Import(name)
		//		if pkg != nil && pkg.Complete() {
		//			im.w.gcimporter[name] = pkg
		//			return
		//		}
	}

	pkg, _, err = im.w.Import(im.dir, name, NewPkgConfig(true, false), nil)
	return pkg, err
}

func (w *PkgWalker) parseFile(dir, file string) (*ast.File, error) {
	return w.parseFileEx(dir, file, nil, 0, w.findMode.Doc)
}

func (w *PkgWalker) parseFileEx(dir, file string, src interface{}, mtime int64, findDoc bool) (*ast.File, error) {
	filename := filepath.Join(dir, file)
	if sd, ok := w.fileSourceData[filename]; ok {
		src = sd.data
		mtime = sd.mtime
	}
	if f, ok := w.ParsedFileCache[filename]; ok {
		if i, ok := w.ParsedFileModTime[filename]; ok {
			if mtime != 0 {
				if mtime == i {
					return f, nil
				}
			} else {
				info, err := os.Stat(filename)
				if err == nil && info.ModTime().UnixNano() == i {
					return f, nil
				}
			}
		}
	}

	flag := parser.AllErrors
	if findDoc {
		flag |= parser.ParseComments
	}
	f, err := parser.ParseFile(w.FileSet, filename, src, flag)
	if f == nil {
		return f, err
	}
	if mtime != 0 {
		w.ParsedFileModTime[filename] = mtime
	} else {
		info, err := os.Stat(filename)
		if err == nil {
			w.ParsedFileModTime[filename] = info.ModTime().UnixNano()
		}
	}
	w.ParsedFileCache[filename] = f
	return f, err
}

func (w *PkgWalker) LookupCursor(pkg *types.Package, conf *PkgConfig, cursor *FileCursor) error {
	f, _ := w.parseFile(cursor.fileDir, cursor.fileName)
	if f != nil {
		cursor.pos = token.Pos(w.FileSet.File(f.Pos()).Base()) + token.Pos(cursor.cursorPos)
		isTest := strings.HasSuffix(cursor.fileName, "_test.go")
		isXTest := false
		if isTest && strings.HasSuffix(f.Name.Name, "_test") {
			isXTest = true
		}
		cursor.xtest = isXTest
	}
	if nm := w.CheckIsName(cursor); nm != nil {
		return w.LookupName(pkg, conf, cursor, nm)
	} else if is := w.CheckIsImport(cursor); is != nil {
		if cursor.xtest {
			return w.LookupImport(conf.XPkg, conf.XInfo, cursor, is)
		} else {
			return w.LookupImport(conf.Pkg, conf.Info, cursor, is)
		}
	} else {
		return w.LookupObjects(conf, cursor)
	}
}

func (w *PkgWalker) LookupName(pkg *types.Package, conf *PkgConfig, cursor *FileCursor, nm *ast.Ident) error {
	if w.findMode.Define {
		w.cmd.Println(w.FileSet.Position(nm.Pos()))
	}
	if w.findMode.Info {
		if cursor.xtest {
			w.cmd.Printf("package %s (%q)\n", pkg.Name()+"_test", pkg.Path())
		} else {
			if pkg.Path() == pkg.Name() {
				w.cmd.Printf("package %s\n", pkg.Name())
			} else {
				w.cmd.Printf("package %s (%q)\n", pkg.Name(), pkg.Path())
			}
		}
	}

	if !w.findMode.Usage {
		return nil
	}
	var usages []int
	findUsage := func(fileMap map[string]*ast.File) {
		for _, f := range fileMap {
			if f != nil && f.Name != nil && f.Name.Name == nm.Name {
				usages = append(usages, int(f.Name.Pos()))
			}
		}
	}
	if cursor.xtest {
		findUsage(conf.XTestFiles)
	} else {
		findUsage(conf.Files)
	}
	(sort.IntSlice(usages)).Sort()
	for _, pos := range usages {
		w.cmd.Println(w.FileSet.Position(token.Pos(pos)))
	}
	return nil
}

func (w *PkgWalker) LookupImport(pkg *types.Package, pkgInfo *types.Info, cursor *FileCursor, is *ast.ImportSpec) error {
	fpath, err := strconv.Unquote(is.Path.Value)
	if err != nil {
		return err
	}

	fbase := fpath
	pos := strings.LastIndexAny(fpath, "./-\\")
	if pos != -1 {
		fbase = fpath[pos+1:]
	}

	var fname string
	if is.Name != nil {
		fname = is.Name.Name
	} else {
		fname = fbase
	}

	var bp *build.Package
	if w.findMode.Define {
		var findpath string = fpath
		//check imported and vendor
		for _, v := range w.Imported {
			vpath := v.Path()
			pos := strings.Index(vpath, "/vendor/")
			if pos >= 0 {
				vpath = vpath[pos+8:]
			}
			if vpath == fpath {
				findpath = v.Path()
				break
			}
		}
		bp, err = w.importPath("", findpath, build.FindOnly)
		if err == nil {
			w.cmd.Println(w.FileSet.Position(is.Pos()).String() + "::" + fname + "::" + fpath + "::" + bp.Dir)
		} else {
			w.cmd.Println(w.FileSet.Position(is.Pos()))
		}
	}

	if w.findMode.Info {
		if fname == fpath {
			w.cmd.Printf("import %s\n", fname)
		} else {
			w.cmd.Printf("import %s (%q)\n", fname, fpath)
		}
	}

	if w.findMode.Doc && bp != nil && bp.Doc != "" {
		w.cmd.Println(bp.Doc)
	}

	if !w.findMode.Usage {
		return nil
	}

	fid := pkg.Path() + "." + fname

	var usages []int
	for id, obj := range pkgInfo.Uses {
		if obj != nil && obj.Id() == fid { //!= nil && cursorObj.Pos() == obj.Pos() {
			if _, ok := obj.(*types.PkgName); ok {
				usages = append(usages, int(id.Pos()))
			}
		}
	}
	(sort.IntSlice(usages)).Sort()
	for _, pos := range usages {
		w.cmd.Println(w.FileSet.Position(token.Pos(pos)))
	}
	return nil
}

func testObjKind(obj types.Object, kind ObjKind) bool {
	k, err := parserObjKind(obj)
	if err != nil {
		return false
	}
	return k == kind
}

func parserObjKind(obj types.Object) (ObjKind, error) {
	var kind ObjKind
	switch t := obj.(type) {
	case *types.PkgName:
		kind = ObjPkgName
	case *types.Const:
		kind = ObjConst
	case *types.TypeName:
		kind = ObjTypeName
		switch t.Type().Underlying().(type) {
		case *types.Interface:
			kind = ObjInterface
		case *types.Struct:
			kind = ObjStruct
		}
	case *types.Var:
		kind = ObjVar
		if t.IsField() {
			kind = ObjField
		}
	case *types.Func:
		kind = ObjFunc
		if sig, ok := t.Type().(*types.Signature); ok {
			if sig.Recv() != nil {
				kind = ObjMethod
			}
		}
	case *types.Label:
		kind = ObjLabel
	case *types.Builtin:
		kind = ObjBuiltin
	case *types.Nil:
		kind = ObjNil
	default:
		return ObjNone, fmt.Errorf("unknown obj type %T", obj)
	}
	return kind, nil
}

func (w *PkgWalker) LookupStructFromField(info *types.Info, cursorPkg *types.Package, cursorObj types.Object, cursorPos token.Pos) types.Object {
	if info == nil {
		conf := NewPkgConfig(true, true)
		_, outconf, _ := w.Import("", cursorPkg.Path(), conf, nil)
		if outconf != nil {
			info = outconf.Info
		}
	}
	for _, obj := range info.Defs {
		if obj == nil {
			continue
		}
		if _, ok := obj.(*types.TypeName); ok {
			if t, ok := obj.Type().Underlying().(*types.Struct); ok {
				for i := 0; i < t.NumFields(); i++ {
					if t.Field(i).Pos() == cursorPos {
						return obj
					}
				}
			}
		}
	}
	return nil
}

func (w *PkgWalker) lookupNamedField(named *types.Named, name string) *types.Named {
	if istruct, ok := named.Underlying().(*types.Struct); ok {
		for i := 0; i < istruct.NumFields(); i++ {
			field := istruct.Field(i)
			if field.Anonymous() {
				fieldType := orgType(field.Type())
				if typ, ok := fieldType.(*types.Named); ok {
					if na := w.lookupNamedField(typ, name); na != nil {
						return na
					}
				}
			} else {
				if field.Name() == name {
					return named
				}
			}
		}
	}
	return nil
}

func (w *PkgWalker) lookupNamedFieldVar(named *types.Named, name string) (*types.Var, *types.Named) {
	if istruct, ok := named.Underlying().(*types.Struct); ok {
		for i := 0; i < istruct.NumFields(); i++ {
			field := istruct.Field(i)
			if field.Anonymous() {
				fieldType := orgType(field.Type())
				if typ, ok := fieldType.(*types.Named); ok {
					if obj, na := w.lookupNamedFieldVar(typ, name); na != nil {
						return obj, na
					}
				}
			} else {
				if field.Name() == name {
					return field, named
				}
			}
		}
	}
	return nil, nil
}

func (w *PkgWalker) lookupNamedMethod(named *types.Named, name string) (types.Object, *types.Named) {
	if iface, ok := named.Underlying().(*types.Interface); ok {
		for i := 0; i < iface.NumMethods(); i++ {
			fn := iface.Method(i)
			if fn.Name() == name {
				if fn.Pkg() != named.Obj().Pkg() {
					goto Embedded
				}
				return fn, named
			}
		}
	Embedded:
		for i := 0; i < iface.NumEmbeddeds(); i++ {
			if obj, na := w.lookupNamedMethod(iface.Embedded(i), name); obj != nil {
				return obj, na
			}
		}
		return nil, nil
	}
	if istruct, ok := named.Underlying().(*types.Struct); ok {
		for i := 0; i < named.NumMethods(); i++ {
			fn := named.Method(i)
			if fn.Name() == name {
				return fn, named
			}
		}
		for i := 0; i < istruct.NumFields(); i++ {
			field := istruct.Field(i)
			if !field.Anonymous() {
				continue
			}
			if typ, ok := field.Type().(*types.Named); ok {
				if obj, na := w.lookupNamedMethod(typ, name); obj != nil {
					return obj, na
				}
			}
		}
	}
	return nil, nil
}

func IsSamePkg(a, b *types.Package) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Path() == b.Path()
}

func IsSameObject(a, b types.Object) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	var apath string
	var bpath string
	if a.Pkg() != nil {
		apath = a.Pkg().Path()
	}
	if b.Pkg() != nil {
		bpath = b.Pkg().Path()
	}
	if apath != bpath {
		return false
	}
	if a.Id() != b.Id() {
		return false
	}
	if a.Type().String() != b.Type().String() {
		return false
	}
	t1, ok1 := a.(*types.TypeName)
	t2, ok2 := b.(*types.TypeName)
	if ok1 && ok2 {
		return t1.Type().String() == t2.Type().String()
	}
	return a.String() == b.String()
}

func orgType(typ types.Type) types.Type {
	if pt, ok := typ.(*types.Pointer); ok {
		return pt.Elem()
	}
	return typ
}

func findScope(s *types.Scope, pos token.Pos) *types.Scope {
	for i := 0; i < s.NumChildren(); i++ {
		child := s.Child(i)
		if child.Contains(pos) {
			return findScope(child, pos)
		}
	}
	return s
}

func (w *PkgWalker) lookupNamed(obj types.Object, cname string) types.Object {
	typ := orgType(obj.Type())
	if typ != nil {
		if name, ok := typ.(*types.Named); ok {
			obj, na := w.lookupNamedFieldVar(name, cname)
			if na != nil {
				return obj
			} else {
				obj, na := w.lookupNamedMethod(name, cname)
				if na != nil {
					return obj
				}
			}
		}
	}
	return nil
}

// pkg=fmt text=Println
func (w *PkgWalker) lookupPackage(pkg *types.Package, text string) types.Object {
	if pkg != nil && pkg.Scope() != nil {
		ids := strings.Split(text, ".")
		obj := pkg.Scope().Lookup(ids[0])
		cursorObj := obj
		if obj != nil {
			var n int = 1
			for n < len(ids) {
				obj = w.lookupNamed(obj, ids[n])
				if obj == nil {
					break
				}
				cursorObj = obj
				n++
			}
		}
		return cursorObj
	}
	return nil
}

func (w *PkgWalker) LookupByText(pkgInfo *types.Info, text string) types.Object {
	var cursorObj types.Object
	ids := strings.Split(text, ".")
	if len(ids) >= 2 {
		//check pkg.Name
		for _, obj := range pkgInfo.Uses {
			if obj.Pkg() != nil {
				if obj.Pkg().Name()+"."+obj.Name() == text {
					cursorObj = obj
					break
				}
			}
		}
		if cursorObj == nil {
			//check local obj.name.name
			for _, obj := range pkgInfo.Defs {
				if obj != nil && obj.Name() == ids[0] {
					var n int = 1
					for n < len(ids) {
						obj = w.lookupNamed(obj, ids[n])
						if obj == nil {
							break
						}
						cursorObj = obj
						n++
					}
				}
			}
		}
		if cursorObj == nil {
			for _, obj := range pkgInfo.Implicits {
				if obj != nil && obj.Name() == ids[0] {
					if pkg, ok := obj.(*types.PkgName); ok {
						cursorObj = w.lookupPackage(pkg.Imported(), strings.Join(ids[1:], "."))
					}
				}
			}
		}
	}
	//check local obj
	if cursorObj == nil {
		for _, obj := range pkgInfo.Defs {
			if obj != nil && obj.Name() == text {
				cursorObj = obj
				break
			}
		}
	}
	//check implicitly declared objects
	if cursorObj == nil {
		for _, obj := range pkgInfo.Implicits {
			if obj != nil && obj.Name() == text {
				cursorObj = obj
				break
			}
		}
	}
	return cursorObj
}

func (w *PkgWalker) LookupObjects(conf *PkgConfig, cursor *FileCursor) error {
	var cursorObj types.Object
	var cursorSelection *types.Selection
	var cursorObjIsDef bool
	//lookup defs

	var pkg *types.Package
	var pkgInfo *types.Info
	if cursor.xtest {
		pkgInfo = conf.XInfo
		pkg = conf.XPkg
	} else {
		pkgInfo = conf.Info
		pkg = conf.Pkg
	}

	_ = cursorObjIsDef
	if cursorObj == nil {
		for sel, obj := range pkgInfo.Selections {
			if cursor.pos >= sel.Sel.Pos() && cursor.pos <= sel.Sel.End() {
				cursorObj = obj.Obj()
				cursorSelection = obj
				break
			}
		}
	}
	var cursorId *ast.Ident
	if cursorObj == nil {
		for id, obj := range pkgInfo.Defs {
			if cursor.pos >= id.Pos() && cursor.pos <= id.End() {
				cursorObj = obj
				cursorId = id
				cursorObjIsDef = true
				break
			}
		}
	}
	_ = cursorSelection
	if cursorObj == nil {
		for id, obj := range pkgInfo.Uses {
			if cursor.pos >= id.Pos() && cursor.pos <= id.End() {
				cursorObj = obj
				break
			}
		}
	}
	if cursorObj == nil {
		for id, obj := range pkgInfo.Implicits {
			if cursor.pos >= id.Pos() && cursor.pos <= id.End() {
				cursorObj = obj
				break
			}
		}
	}
	if cursorObj == nil && cursor.text != "" {
		cursorObj = w.LookupByText(pkgInfo, cursor.text)
	}

	var kind ObjKind
	if cursorObj != nil {
		var err error
		kind, err = parserObjKind(cursorObj)
		if err != nil {
			return err
		}
	} else if cursorId != nil {
		kind = ObjImplicit
	} else {
		//TODO
		return fmt.Errorf("nof find object %v:%v", cursor.fileName, cursor.pos)
	}
	if kind == ObjField {
		if cursorObj.(*types.Var).Anonymous() {
			typ := orgType(cursorObj.Type())
			if named, ok := typ.(*types.Named); ok {
				cursorObj = named.Obj()
			}
		}
	}

	var cursorPkg *types.Package
	var cursorPos token.Pos

	if cursorObj != nil {
		cursorPkg = cursorObj.Pkg()
		cursorPos = cursorObj.Pos()
	} else {
		cursorPkg = pkg
		cursorPos = cursorId.Pos()
	}
	//var fieldTypeInfo *types.Info
	var fieldTypeObj types.Object
	//	if cursorPkg == pkg {
	//		fieldTypeInfo = pkgInfo
	//	}
	cursorIsInterfaceMethod := false
	var cursorInterfaceTypeName string
	var cursorInterfaceTypeNamed *types.Named

	if kind == ObjMethod && cursorSelection != nil && cursorSelection.Recv() != nil {
		sig := cursorObj.(*types.Func).Type().Underlying().(*types.Signature)
		if _, ok := sig.Recv().Type().Underlying().(*types.Interface); ok {
			if named, ok := cursorSelection.Recv().(*types.Named); ok {
				obj, na := w.lookupNamedMethod(named, cursorObj.Name())
				if obj != nil && na != nil {
					cursorObj = obj
					cursorPkg = na.Obj().Pkg()
					cursorInterfaceTypeName = na.Obj().Name()
					cursorInterfaceTypeNamed = na
					cursorIsInterfaceMethod = true
				}
			}
		}
	} else if kind == ObjField && cursorSelection != nil {
		if recv := cursorSelection.Recv(); recv != nil {
			typ := orgType(recv)
			if typ != nil {
				if name, ok := typ.(*types.Named); ok {
					fieldTypeObj = name.Obj()
					na := w.lookupNamedField(name, cursorObj.Name())
					if na != nil {
						fieldTypeObj = na.Obj()
					}
					//check current pkg
					if fieldTypeObj != nil && fieldTypeObj.Pkg() == pkg {
						cursorPkg = fieldTypeObj.Pkg()
						if t, ok := fieldTypeObj.Type().Underlying().(*types.Struct); ok {
							for i := 0; i < t.NumFields(); i++ {
								if t.Field(i).Id() == cursorObj.Id() {
									cursorPos = t.Field(i).Pos()
									break
								}
							}
						}
					}
				}
			}
		}
	}
	if typesVerbose {
		w.cmd.Println("parser", cursorObj, kind, cursorIsInterfaceMethod)
	}
	if cursorPkg != nil && cursorPkg != pkg &&
		kind != ObjPkgName && w.isBinaryPkg(cursorPkg.Path()) {
		pkg, conf, _ := w.Import("", cursorPkg.Path(), NewPkgConfig(true, true), nil)
		if pkg != nil {
			if cursorIsInterfaceMethod {
				for k, v := range conf.Info.Defs {
					if k != nil && v != nil && IsSameObject(v, cursorInterfaceTypeNamed.Obj()) {
						named := v.Type().(*types.Named)
						obj, typ := w.lookupNamedMethod(named, cursorObj.Name())
						if obj != nil && typ != nil {
							cursorObj = obj
							cursorPos = obj.Pos()
							cursorPkg = typ.Obj().Pkg()
							cursorInterfaceTypeName = typ.Obj().Name()
							break
						}
					}
				}
				// for _, obj := range conf.Info.Defs {
				// 	if obj == nil {
				// 		continue
				// 	}
				// 	if fn, ok := obj.(*types.Func); ok {
				// 		if fn.Name() == cursorObj.Name() {
				// 			if sig, ok := fn.Type().Underlying().(*types.Signature); ok {
				// 				if named, ok := sig.Recv().Type().(*types.Named); ok {
				// 					if named.Obj() != nil && named.Obj().Name() == cursorInterfaceTypeName {
				// 						cursorPos = obj.Pos()
				// 						break
				// 					}
				// 				}
				// 			}
				// 		}
				// 	}
				// }
			} else if kind == ObjField && fieldTypeObj != nil {
				for _, obj := range conf.Info.Defs {
					if obj == nil {
						continue
					}
					if _, ok := obj.(*types.TypeName); ok {
						if IsSameObject(fieldTypeObj, obj) {
							if t, ok := obj.Type().Underlying().(*types.Struct); ok {
								for i := 0; i < t.NumFields(); i++ {
									if t.Field(i).Id() == cursorObj.Id() {
										cursorPos = t.Field(i).Pos()
										break
									}
								}
							}
							break
						}
					}
				}
			} else {
				for k, v := range conf.Info.Defs {
					if k != nil && v != nil && IsSameObject(v, cursorObj) {
						cursorPos = k.Pos()
						break
					}
				}
			}
		}
		//		if kind == ObjField || cursorIsInterfaceMethod {
		//			fieldTypeInfo = conf.Info
		//		}
	}
	//	if kind == ObjField {
	//		fieldTypeObj = w.LookupStructFromField(fieldTypeInfo, cursorPkg, cursorObj, cursorPos)
	//	}
	if w.findMode.Define {
		w.cmd.Println(w.FileSet.Position(cursorPos))
	}
	if w.findMode.Info {
		/*if kind == ObjField && fieldTypeObj != nil {
			typeName := fieldTypeObj.Name()
			if fieldTypeObj.Pkg() != nil && fieldTypeObj.Pkg() != pkg {
				typeName = fieldTypeObj.Pkg().Name() + "." + fieldTypeObj.Name()
			}
			fmt.Println(typeName, simpleObjInfo(cursorObj))
		} else */
		if kind == ObjBuiltin {
			w.cmd.Println(builtinInfo(cursorObj.Name()))
		} else if kind == ObjPkgName {
			w.cmd.Println(cursorObj.String())
		} else if kind == ObjImplicit {
			w.cmd.Printf("%s is implicit\n", cursorId.Name)
		} else if cursorIsInterfaceMethod {
			w.cmd.Println(strings.Replace(simpleObjInfo(cursorObj), "(interface)", cursorPkg.Name()+"."+cursorInterfaceTypeName, 1))
		} else {
			w.cmd.Println(simpleObjInfo(cursorObj))
		}
	}

	if w.findMode.Doc && w.findMode.Define {
		pos := w.FileSet.Position(cursorPos)
		file := w.ParsedFileCache[pos.Filename]
		if file != nil {
			line := pos.Line
			var group *ast.CommentGroup
			for _, v := range file.Comments {
				lastLine := w.FileSet.Position(v.End()).Line
				if lastLine == line || lastLine == line-1 {
					group = v
				} else if lastLine > line {
					break
				}
			}
			if group != nil {
				w.cmd.Println(group.Text())
			}
		}
	}
	if !w.findMode.Usage {
		return nil
	}

	var usages []int
	if kind == ObjPkgName {
		for id, obj := range pkgInfo.Uses {
			if obj != nil && obj.Id() == cursorObj.Id() { //!= nil && cursorObj.Pos() == obj.Pos() {
				if _, ok := obj.(*types.PkgName); ok {
					usages = append(usages, int(id.Pos()))
				}
			}
		}
	} else {
		//		for id, obj := range pkgInfo.Defs {
		//			if obj == cursorObj { //!= nil && cursorObj.Pos() == obj.Pos() {
		//				usages = append(usages, int(id.Pos()))
		//			}
		//		}
		if cursorObj != nil {
			for id, obj := range pkgInfo.Uses {
				if obj == cursorObj { //!= nil && cursorObj.Pos() == obj.Pos() {
					usages = append(usages, int(id.Pos()))
				}
			}
		} else {
			for id, obj := range pkgInfo.Uses {
				if obj != nil && obj.Pos() == cursorPos { //!= nil && cursorObj.Pos() == obj.Pos() {
					usages = append(usages, int(id.Pos()))
				}
			}
		}
	}
	var pkg_path string
	var xpkg_path string
	if conf.Pkg != nil {
		pkg_path = conf.Pkg.Path()
	}
	if conf.XPkg != nil {
		xpkg_path = conf.XPkg.Path()
	}

	if cursorPkg != nil &&
		(cursorPkg.Path() == pkg_path || cursorPkg.Path() == xpkg_path) &&
		kind != ObjPkgName {
		usages = append(usages, int(cursorPos))
	}

	(sort.IntSlice(usages)).Sort()
	for _, pos := range usages {
		w.cmd.Println(w.FileSet.Position(token.Pos(pos)))
	}
	//check look for current pkg.object on pkg_test
	if w.findMode.UsageAll || IsSamePkg(cursorPkg, conf.Pkg) {
		var addInfo *types.Info
		if cursor.xtest {
			addInfo = conf.Info
		} else {
			addInfo = conf.XInfo
		}
		if addInfo != nil && cursorPkg != nil {
			var usages []int
			//		for id, obj := range addInfo.Defs {
			//			if id != nil && obj != nil && obj.Id() == cursorObj.Id() {
			//				usages = append(usages, int(id.Pos()))
			//			}
			//		}
			for k, v := range addInfo.Uses {
				if k != nil && v != nil && IsSameObject(v, cursorObj) {
					usages = append(usages, int(k.Pos()))
				}
			}
			(sort.IntSlice(usages)).Sort()
			for _, pos := range usages {
				w.cmd.Println(w.FileSet.Position(token.Pos(pos)))
			}
		}
	}
	if !w.findMode.UsageAll {
		return nil
	}

	if cursorPkg == nil {
		return nil
	}

	var find_def_pkg string
	var uses_paths []string

	if cursorPkg.Path() != pkg_path && cursorPkg.Path() != xpkg_path {
		find_def_pkg = cursorPkg.Path()
		if w.findMode.SkipGoroot {
			bp, err := w.importPath(conf.Bpkg.Dir, find_def_pkg, 0)
			if err == nil && !bp.Goroot {
				uses_paths = append(uses_paths, find_def_pkg)
			}
		} else {
			uses_paths = append(uses_paths, find_def_pkg)
		}
	}

	cursorPkgPath := cursorObj.Pkg().Path()
	if w.ModPkg == nil && pkgutil.IsVendorExperiment() {
		cursorPkgPath = pkgutil.VendorPathToImportPath(cursorPkgPath)
	}
	// check on module dir
	if w.ModPkg != nil {
		dir := w.ModPkg.Node().ModDir()
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if !info.IsDir() {
				return nil
			}
			if path != dir && info.Name() == "vendor" {
				return filepath.SkipDir
			}
			if conf.Bpkg.Dir == path {
				return nil
			}
			bp, err := w.importPath(dir, path, 0)
			if err != nil {
				return nil
			}
			if !bp.IsCommand() {
				importPath := filepath.Join(w.ModPkg.Node().Path(), path[len(dir)+1:])
				if importPath == cursorPkgPath {
					return nil
				}
			}
			find := false
			for _, v := range bp.Imports {
				if v == cursorPkgPath {
					find = true
					break
				}
			}
			if find {
				importPath := path //filepath.Join(w.mod.Path(), path[len(dir)+1:])
				for _, v := range uses_paths {
					if v == importPath {
						return nil
					}
				}
				uses_paths = append(uses_paths, importPath)
			}
			return nil
		})
	}
	ctx := *w.Context
	searchAll := true
	if w.ModPkg != nil {
		ctx.GOPATH = ""
		if w.findMode.SkipGoroot {
			searchAll = false
		}
	}
	if searchAll {
		buildutil.ForEachPackage(&ctx, func(importPath string, err error) {
			if err != nil {
				return
			}
			if importPath == conf.Pkg.Path() {
				return
			}
			bp, err := w.importPath("", importPath, 0)
			if err != nil {
				return
			}
			find := false
			if bp.ImportPath == cursorPkg.Path() {
				find = true
			} else {
				for _, v := range bp.Imports {
					if v == cursorPkgPath {
						find = true
						break
					}
				}
			}
			if find {
				for _, v := range uses_paths {
					if v == bp.ImportPath {
						return
					}
				}
				if w.findMode.SkipGoroot && bp.Goroot {
					return
				}
				uses_paths = append(uses_paths, bp.ImportPath)
			}
		})
	}

	//w.Imported = make(map[string]*types.Package)

	for _, v := range uses_paths {
		var usages []int
		vpkg, conf, _ := w.Import("", v, NewPkgConfig(false, true), nil)
		if vpkg != nil && vpkg != pkg {
			if conf.Info != nil {
				for k, v := range conf.Info.Uses {
					if k != nil && v != nil && IsSameObject(v, cursorObj) {
						usages = append(usages, int(k.Pos()))
					}
				}
			}
			if conf.XInfo != nil {
				for k, v := range conf.XInfo.Uses {
					if k != nil && v != nil && IsSameObject(v, cursorObj) {
						usages = append(usages, int(k.Pos()))
					}
				}
			}
		}
		if v == find_def_pkg {
			usages = append(usages, int(cursorPos))
		}
		(sort.IntSlice(usages)).Sort()
		for _, pos := range usages {
			w.cmd.Println(w.FileSet.Position(token.Pos(pos)))
		}
	}
	return nil
}

func (w *PkgWalker) CheckIsName(cursor *FileCursor) *ast.Ident {
	if cursor.fileDir == "" {
		return nil
	}
	file, _ := w.parseFile(cursor.fileDir, cursor.fileName)
	if file == nil {
		return nil
	}
	if inRange(file.Name, cursor.pos) {
		return file.Name
	}
	return nil
}

func (w *PkgWalker) CheckIsImport(cursor *FileCursor) *ast.ImportSpec {
	if cursor.fileDir == "" {
		return nil
	}
	file, _ := w.parseFile(cursor.fileDir, cursor.fileName)
	if file == nil {
		return nil
	}
	for _, is := range file.Imports {
		if inRange(is, cursor.pos) {
			return is
		}
	}
	return nil
}

func inRange(node ast.Node, p token.Pos) bool {
	if node == nil {
		return false
	}
	return p >= node.Pos() && p <= node.End()
}

func (w *PkgWalker) nodeString(node interface{}) string {
	if node == nil {
		return ""
	}
	var b bytes.Buffer
	printer.Fprint(&b, w.FileSet, node)
	return b.String()
}
