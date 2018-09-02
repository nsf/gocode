package main

//-------------------------------------------------------------------------
// gc_ibin_parser
//
// The following part of the code may contain portions of the code from the Go
// standard library, which tells me to retain their copyright notice:
//
// Copyright (c) 2012 The Go Authors. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//-------------------------------------------------------------------------

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"io"
	"sort"
	"strings"
)

type intReader struct {
	*bytes.Reader
}

func (r *intReader) int64() int64 {
	i, err := binary.ReadVarint(r.Reader)
	if err != nil {
		panic(fmt.Sprintf("read varint error: %v", err))
	}
	return i
}

func (r *intReader) uint64() uint64 {
	i, err := binary.ReadUvarint(r.Reader)
	if err != nil {
		panic(fmt.Sprintf("read varint error: %v", err))
	}
	return i
}

type gc_ibin_parser struct {
	data     []byte
	version  int
	callback func(pkg string, decl ast.Decl)
	pfc      *package_file_cache

	stringData  []byte
	stringCache map[uint64]string
	declData    []byte
	typCache    map[uint64]*ibinType
	pkgCache    map[uint64]ibinPackage
}

type ibinPackage struct {
	fullName string
	index    map[string]uint64
	declTyp  map[string]*ibinType
}

func (p *gc_ibin_parser) typAt(off uint64) *ibinType {
	if t, ok := p.typCache[off]; ok {
		return t
	}

	if off < predeclReserved {
		panic(fmt.Sprintf("predeclared type missing from cache: %v", off))
	}

	r := &importReader{p: p}
	r.declReader.Reset(p.declData[off-predeclReserved:])
	t := r.doType()
	p.typCache[off] = t
	return t
}

func (p *gc_ibin_parser) stringAt(off uint64) string {
	if s, ok := p.stringCache[off]; ok {
		return s
	}

	slen, n := binary.Uvarint(p.stringData[off:])
	if n <= 0 {
		panic(fmt.Sprintf("varint failed"))
	}
	spos := off + uint64(n)
	s := string(p.stringData[spos : spos+slen])
	p.stringCache[off] = s
	return s
}

func (p *gc_ibin_parser) pkgAt(off uint64) ibinPackage {
	if pkg, ok := p.pkgCache[off]; ok {
		return pkg
	}
	path := p.stringAt(off)
	panic(fmt.Sprintf("missing package %q", path))
}

func (p *gc_ibin_parser) init(data []byte, pfc *package_file_cache) {
	p.data = data
	p.version = -1
	p.pfc = pfc
	p.stringCache = make(map[uint64]string)
	p.pkgCache = make(map[uint64]ibinPackage)
}

func (p *gc_ibin_parser) parse_export(callback func(string, ast.Decl)) {
	const currentVersion = 0
	p.callback = callback

	r := &intReader{bytes.NewReader(p.data)}
	p.version = int(r.uint64())
	if p.version != currentVersion {
		panic(fmt.Errorf("unknown export format version %d", p.version))
	}

	sLen := int64(r.uint64())
	dLen := int64(r.uint64())
	whence, _ := r.Seek(0, io.SeekCurrent)
	p.stringData = p.data[whence : whence+sLen]

	p.declData = p.data[whence+sLen : whence+sLen+dLen]
	r.Seek(sLen+dLen, io.SeekCurrent)

	// built-in types
	p.typCache = make(map[uint64]*ibinType)
	for i, pt := range predeclared {
		p.typCache[uint64(i)] = &ibinType{typ: pt}
	}

	pkgs := make([]ibinPackage, r.uint64())
	for i := range pkgs {
		pkgPathOff := r.uint64()
		pkgPath := p.stringAt(pkgPathOff)
		pkgName := p.stringAt(r.uint64())
		_ = r.uint64() // package height; unused here

		var fullName string
		if pkgPath == "" {
			// imported package
			fullName = "!" + p.pfc.name + "!" + pkgName
			p.pfc.defalias = fullName[strings.LastIndex(fullName, "!")+1:]
		} else {
			// third party import
			fullName = "!" + pkgPath + "!" + pkgName
			p.pfc.add_package_to_scope(fullName, pkgPath)
		}

		// list of package entities pointing at decl data by name
		index := map[string]uint64{}
		nSyms := int(r.uint64())
		for i := 0; i < nSyms; i++ {
			name := p.stringAt(r.uint64())
			index[name] = r.uint64()
		}

		pkg := ibinPackage{fullName, index, make(map[string]*ibinType)}
		p.pkgCache[pkgPathOff] = pkg
		pkgs[i] = pkg
	}

	for _, pkg := range pkgs {
		names := make([]string, 0, len(pkg.index))
		for name := range pkg.index {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			p.doDecl(pkg, name)
		}
	}
}

func (p *gc_ibin_parser) doDecl(pkg ibinPackage, name string) *ibinType {
	if t, ok := pkg.declTyp[name]; ok { // already processed
		return t
	}

	off, ok := pkg.index[name]
	if !ok {
		panic(fmt.Sprintf("%q not in %q", name, pkg.fullName))
	}

	r := &importReader{p: p, currPkg: pkg}
	r.declReader.Reset(p.declData[off:])
	t := r.obj(name)
	pkg.declTyp[name] = t
	return t
}

type ibinType struct {
	typ ast.Expr
	und *ibinType
}

func (t *ibinType) underlying() ast.Expr {
	for t.und != nil {
		t = t.und
	}
	return t.typ
}

type importReader struct {
	p          *gc_ibin_parser
	declReader bytes.Reader
	currPkg    ibinPackage
}

func (r *importReader) obj(name string) *ibinType {
	tag := r.byte()
	r.pos()

	switch tag {
	case 'A':
		typ := r.typ()
		r.p.callback(r.currPkg.fullName, &ast.GenDecl{
			Tok:   token.TYPE,
			Specs: []ast.Spec{typeAliasSpec(name, typ.typ)},
		})
		return typ
	case 'C':
		typ := r.value()
		r.p.callback(r.currPkg.fullName, &ast.GenDecl{
			Tok: token.CONST,
			Specs: []ast.Spec{
				&ast.ValueSpec{
					Names:  []*ast.Ident{ast.NewIdent(name)},
					Type:   typ.typ,
					Values: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "0"}},
				},
			},
		})
		return typ
	case 'F':
		sig := r.signature()
		r.p.callback(r.currPkg.fullName, &ast.FuncDecl{
			Name: ast.NewIdent(name),
			Type: sig,
		})
		return &ibinType{typ: sig}
	case 'T':
		// Types can be recursive. We need to setup a stub
		// declaration before recursing.
		t := &ibinType{typ: &ast.SelectorExpr{X: ast.NewIdent(r.currPkg.fullName), Sel: ast.NewIdent(name)}}
		r.currPkg.declTyp[name] = t
		t.und = r.p.typAt(r.uint64())
		r.p.callback(r.currPkg.fullName, &ast.GenDecl{
			Tok: token.TYPE,
			Specs: []ast.Spec{
				&ast.TypeSpec{
					Name: ast.NewIdent(name),
					Type: t.und.typ,
				},
			},
		})

		if _, ok := t.und.typ.(*ast.InterfaceType); ok { // interfaces cannot have methods
			return t
		}

		// read associated methods
		for n := r.uint64(); n > 0; n-- {
			r.pos()
			mname := r.ident()
			recv := &ast.FieldList{List: []*ast.Field{r.param()}}
			msig := r.signature()
			strip_method_receiver(recv)
			r.p.callback(r.currPkg.fullName, &ast.FuncDecl{
				Recv: recv,
				Name: ast.NewIdent(mname),
				Type: msig,
			})
		}
		return t

	case 'V':
		typ := r.typ()
		r.p.callback(r.currPkg.fullName, &ast.GenDecl{
			Tok: token.VAR,
			Specs: []ast.Spec{
				&ast.ValueSpec{
					Names: []*ast.Ident{ast.NewIdent(name)},
					Type:  typ.typ,
				},
			},
		})
		return typ
	default:
		panic(fmt.Sprintf("unexpected tag: %v", tag))
	}
}

const predeclReserved = 32

type itag uint64

const (
	// Types
	definedType itag = iota
	pointerType
	sliceType
	arrayType
	chanType
	mapType
	signatureType
	structType
	interfaceType
)

// we don't care about that, let's just skip it
func (r *importReader) pos() {
	if r.int64() != deltaNewFile {
	} else if l := r.int64(); l == -1 {
	} else {
		r.string()
	}
}

func (r *importReader) value() *ibinType {
	t := r.typ()
	typ := t.underlying()
	ident, ok := typ.(*ast.Ident)
	if !ok {
		panic(fmt.Sprintf("unexpected type: %v", typ))
	}

	switch ident.Name {
	case "bool", "&untypedBool&":
		r.bool()
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16",
		"uint32", "uint64", "uintptr", "byte", "rune", "&untypedInt&", "&untypedRune&":
		r.mpint(ident)
	case "float32", "float64", "&untypedFloat&":
		r.mpfloat(ident)
	case "complex64", "complex128", "&untypedComplex&":
		r.mpfloat(ident)
		r.mpfloat(ident)
	case "string", "&untypedString&":
		r.string()
	default:
		panic(fmt.Sprintf("unexpected type: %v", typ))
	}
	return t
}

func intSize(typ *ast.Ident) (signed bool, maxBytes uint) {
	if typ.Name[0] == '&' { // untyped
		return true, 64
	}

	switch typ.Name {
	case "float32", "complex64":
		return true, 3
	case "float64", "complex128":
		return true, 7
	case "int8":
		return true, 1
	case "int16":
		return true, 2
	case "int32", "rune":
		return true, 4
	case "int64", "int":
		return true, 8
	case "uint8", "byte":
		return false, 1
	case "uint16":
		return false, 2
	case "uint32":
		return false, 4
	case "uint64", "uint", "uintptr":
		return false, 8
	}
	panic(fmt.Sprintf("unexpected type: %v", typ))
}

func (r *importReader) mpint(typ *ast.Ident) constant.Value {
	signed, maxBytes := intSize(typ)

	maxSmall := 256 - maxBytes
	if signed {
		maxSmall = 256 - 2*maxBytes
	}
	if maxBytes == 1 {
		maxSmall = 256
	}

	n, _ := r.declReader.ReadByte()
	if uint(n) < maxSmall {
		v := int64(n)
		if signed {
			v >>= 1
			if n&1 != 0 {
				v = ^v
			}
		}
		return constant.MakeInt64(v)
	}

	v := -n
	if signed {
		v = -(n &^ 1) >> 1
	}
	if v < 1 || uint(v) > maxBytes {
		panic(fmt.Sprintf("weird decoding: %v, %v => %v", n, signed, v))
	}

	buf := make([]byte, v)
	io.ReadFull(&r.declReader, buf)

	// convert to little endian
	// TODO(gri) go/constant should have a more direct conversion function
	//           (e.g., once it supports a big.Float based implementation)
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}

	x := constant.MakeFromBytes(buf)
	if signed && n&1 != 0 {
		x = constant.UnaryOp(token.SUB, x, 0)
	}
	return x
}

func (r *importReader) mpfloat(typ *ast.Ident) {
	x := r.mpint(typ)
	if constant.Sign(x) == 0 {
		return
	}
	r.int64()
}

func (r *importReader) doType() *ibinType {
	k := r.kind()
	switch k {
	default:
		panic(fmt.Sprintf("unexpected kind tag: %v", k))
	case definedType:
		pkg, name := r.qualifiedIdent()
		r.p.doDecl(pkg, name)
		return pkg.declTyp[name]
	case pointerType:
		elt := r.typ()
		return &ibinType{typ: &ast.StarExpr{X: elt.typ}}
	case sliceType:
		elt := r.typ()
		return &ibinType{typ: &ast.ArrayType{Len: nil, Elt: elt.typ}}
	case arrayType:
		n := r.uint64()
		elt := r.typ()
		return &ibinType{typ: &ast.ArrayType{
			Len: &ast.BasicLit{Kind: token.INT, Value: fmt.Sprint(n)},
			Elt: elt.typ,
		}}
	case chanType:
		dir := ast.SEND | ast.RECV
		switch d := r.uint64(); d {
		case 1:
			dir = ast.RECV
		case 2:
			dir = ast.SEND
		case 3:
			// already set
		default:
			panic(fmt.Sprintf("unexpected channel dir %d", d))
		}
		elt := r.typ()
		return &ibinType{typ: &ast.ChanType{Dir: dir, Value: elt.typ}}
	case mapType:
		key := r.typ()
		val := r.typ()
		return &ibinType{typ: &ast.MapType{Key: key.typ, Value: val.typ}}
	case signatureType:
		r.currPkg = r.pkg()
		return &ibinType{typ: r.signature()}

	case structType:
		r.currPkg = r.pkg()

		fields := make([]*ast.Field, r.uint64())
		for i := range fields {
			r.pos()
			fname := r.ident()
			ftyp := r.typ()
			emb := r.bool()
			r.string()
			var names []*ast.Ident
			if fname != "" && !emb {
				names = []*ast.Ident{ast.NewIdent(fname)}
			}
			fields[i] = &ast.Field{Names: names, Type: ftyp.typ}
		}
		return &ibinType{typ: &ast.StructType{Fields: &ast.FieldList{List: fields}}}

	case interfaceType:
		r.currPkg = r.pkg()

		numEmbeds := int(r.uint64())
		embeddeds := make([]*ast.SelectorExpr, 0, numEmbeds)
		for i := 0; i < numEmbeds; i++ {
			r.pos()
			t := r.typ()
			if named, ok := t.typ.(*ast.SelectorExpr); ok {
				embeddeds = append(embeddeds, named)
			}
		}

		methods := make([]*ast.Field, r.uint64())
		for i := range methods {
			r.pos()
			mname := r.ident()
			msig := r.signature()
			methods[i] = &ast.Field{
				Names: []*ast.Ident{ast.NewIdent(mname)},
				Type:  msig,
			}
		}
		for _, field := range embeddeds {
			methods = append(methods, &ast.Field{Type: field})
		}

		return &ibinType{typ: &ast.InterfaceType{Methods: &ast.FieldList{List: methods}}}
	}
}

func (r *importReader) signature() *ast.FuncType {
	params := r.paramList()
	results := r.paramList()
	if params != nil && len(params.List) > 0 {
		if r.bool() { // variadic flag
			last := params.List[len(params.List)-1]
			last.Type = &ast.Ellipsis{Elt: last.Type.(*ast.ArrayType).Elt}
		}
	}
	return &ast.FuncType{Params: params, Results: results}
}

func (r *importReader) paramList() *ast.FieldList {
	xs := make([]*ast.Field, r.uint64())
	for i := range xs {
		xs[i] = r.param()
	}
	return &ast.FieldList{List: xs}
}

func (r *importReader) param() *ast.Field {
	r.pos()
	name := r.ident()
	if name == "" { // gocode specific hack for unnamed parameters
		name = "?"
	}
	t := r.typ()
	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  t.typ,
	}
}

func (r *importReader) typ() *ibinType   { return r.p.typAt(r.uint64()) }
func (r *importReader) kind() itag       { return itag(r.uint64()) }
func (r *importReader) pkg() ibinPackage { return r.p.pkgAt(r.uint64()) }
func (r *importReader) string() string   { return r.p.stringAt(r.uint64()) }
func (r *importReader) bool() bool       { return r.uint64() != 0 }
func (r *importReader) ident() string    { return r.string() }

func (r *importReader) qualifiedIdent() (ibinPackage, string) {
	name := r.string()
	pkg := r.pkg()
	return pkg, name
}

func (r *importReader) int64() int64 {
	n, err := binary.ReadVarint(&r.declReader)
	if err != nil {
		panic(fmt.Sprintf("readVarint: %v", err))
	}
	return n
}

func (r *importReader) uint64() uint64 {
	n, err := binary.ReadUvarint(&r.declReader)
	if err != nil {
		panic(fmt.Sprintf("readUvarint: %v", err))
	}
	return n
}

func (r *importReader) byte() byte {
	x, err := r.declReader.ReadByte()
	if err != nil {
		panic(fmt.Sprintf("declReader.ReadByte: %v", err))
	}
	return x
}
