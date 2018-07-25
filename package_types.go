package main

import (
	"bytes"
	"go/build"
	"go/importer"
	"go/token"
	"go/types"

	"github.com/visualfc/gotools/pkg/srcimporter"
	"golang.org/x/tools/go/gcexportdata"
)

type types_parser struct {
	pfc *package_file_cache
	pkg *types.Package
}

func (p *types_parser) init(path string, dir string, pfc *package_file_cache, source bool) {
	if source {
		im := srcimporter.New(&build.Default, token.NewFileSet(), make(map[string]*types.Package))
		if dir != "" {
			p.pkg, _ = im.ImportFrom(path, dir, 0)
		} else {
			p.pkg, _ = im.Import(path)
		}
	} else {
		p.pkg, _ = importer.Default().Import(path)
	}
	p.pfc = pfc
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
