package main

import (
	"bytes"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"log"

	pkgwalk "github.com/visualfc/gotools/types"
	"golang.org/x/tools/go/gcexportdata"
)

type types_parser struct {
	pfc *package_file_cache
	pkg *types.Package
}

// func DefaultPkgConfig() *pkgwalk.PkgConfig {
// 	conf := &pkgwalk.PkgConfig{IgnoreFuncBodies: true, AllowBinary: true, WithTestFiles: false}
// 	conf.Info = &types.Info{
// 		Uses:       make(map[*ast.Ident]types.Object),
// 		Defs:       make(map[*ast.Ident]types.Object),
// 		Selections: make(map[*ast.SelectorExpr]*types.Selection),
// 		//Types:      make(map[ast.Expr]types.TypeAndValue),
// 		//Scopes : make(map[ast.Node]*types.Scope)
// 		//Implicits : make(map[ast.Node]types.Object)
// 	}
// 	conf.XInfo = &types.Info{
// 		Uses:       make(map[*ast.Ident]types.Object),
// 		Defs:       make(map[*ast.Ident]types.Object),
// 		Selections: make(map[*ast.SelectorExpr]*types.Selection),
// 	}
// 	return conf
// }

func (p *types_parser) initSource(import_path string, path string, dir string, pfc *package_file_cache, c *auto_complete_context) {
	//conf := &pkgwalk.PkgConfig{IgnoreFuncBodies: true, AllowBinary: false, WithTestFiles: true}
	//	conf.Info = &types.Info{}
	//	conf.XInfo = &types.Info{}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	conf := pkgwalk.DefaultPkgConfig()
	pkg, _, err := c.walker.ImportHelper(".", path, import_path, conf, nil)
	if err != nil {
		log.Println(err)
	}
	p.pkg = pkg
	//	im := srcimporter.New(&build.Default, c.fset, c.packages)
	//	if dir != "" {
	//		p.pkg, _ = im.ImportFrom(path, dir, 0)
	//	} else {
	//		p.pkg, _ = im.Import(path)
	//	}
	p.pfc = pfc
}

func (p *types_parser) initData(path string, data []byte, pfc *package_file_cache, c *auto_complete_context) {
	p.pkg, _ = importer.For("gc", func(path string) (io.ReadCloser, error) {
		return NewMemReadClose(data), nil
	}).Import(path)
	p.pfc = pfc
	if p.pkg != nil {
		c.walker.Imported[p.pkg.Path()] = p.pkg
	}
}

type MemReadClose struct {
	*bytes.Buffer
}

func (m *MemReadClose) Close() error {
	return nil
}

func NewMemReadClose(data []byte) *MemReadClose {
	return &MemReadClose{bytes.NewBuffer(data)}
}

func (p *types_parser) exportData() []byte {
	if p.pkg == nil {
		return nil
	}
	fset := token.NewFileSet()
	var buf bytes.Buffer
	gcexportdata.Write(&buf, fset, p.pkg)
	return buf.Bytes()
}
