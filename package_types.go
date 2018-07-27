package main

import (
	"bytes"
	"go/build"
	"go/importer"
	"go/token"
	"go/types"
	"io"

	"github.com/visualfc/gotools/pkg/srcimporter"
	"golang.org/x/tools/go/gcexportdata"
)

type types_parser struct {
	pfc *package_file_cache
	pkg *types.Package
}

func (p *types_parser) initSource(path string, dir string, pfc *package_file_cache) {
	im := srcimporter.New(&build.Default, token.NewFileSet(), make(map[string]*types.Package))
	if dir != "" {
		p.pkg, _ = im.ImportFrom(path, dir, 0)
	} else {
		p.pkg, _ = im.Import(path)
	}
	p.pfc = pfc
}

func (p *types_parser) initData(path string, data []byte, pfc *package_file_cache) {
	p.pkg, _ = importer.For("gc", func(path string) (io.ReadCloser, error) {
		return NewMemReadClose(data), nil
	}).Import(path)
	p.pfc = pfc
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
