package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/token"
	"go/types"
	"log"
	"regexp"
	"strings"

	"github.com/visualfc/gotools/pkg/srcimporter"
	"golang.org/x/tools/go/gcexportdata"
)

type types_parser struct {
	pfc *package_file_cache
	pkg *types.Package
}

func typeToAst(typ types.Type) ast.Expr {
	switch t := (typ).(type) {
	case *types.Basic:
		return ast.NewIdent(t.Name())
	case *types.Named:
		switch ut := t.Underlying().(type) {
		case *types.Basic:
			return ast.NewIdent(typ.String())
		case *types.Interface:
		case *types.Struct:
		default:
			log.Fatalf("%T\n", ut)
		}
	}
	return nil
}

func objToAst(obj types.Object) ast.Decl {
	switch ot := (obj).(type) {
	case *types.Const:
		typ := typeToAst(ot.Type())
		return &ast.GenDecl{
			Tok: token.CONST,
			Specs: []ast.Spec{
				&ast.ValueSpec{
					Names:  []*ast.Ident{ast.NewIdent(obj.Name())},
					Type:   typ,
					Values: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: ot.Val().String()}},
				},
			},
		}
	case *types.Var:
		typ := typeToAst(ot.Type())
		return &ast.GenDecl{
			Tok: token.VAR,
			Specs: []ast.Spec{
				&ast.ValueSpec{
					Names: []*ast.Ident{ast.NewIdent(obj.Name())},
					Type:  typ,
				},
			},
		}
	default:
		//fmt.Println(typ)
		_ = ot
	}
	return nil
}

func (p *types_parser) init(path string, dir string, pfc *package_file_cache, source bool) {
	if source {
		im := srcimporter.New(&build.Default, token.NewFileSet(), make(map[string]*types.Package))
		if dir != "" {
			var err error
			p.pkg, err = im.ImportFrom(path, dir, 0)
			log.Println(err, dir)
		} else {
			p.pkg, _ = im.Import(path)
		}
	} else {
		p.pkg, _ = importer.Default().Import(path)
	}
	p.pfc = pfc
}

func MakeTuple(re *regexp.Regexp, params *types.Tuple) string {
	var info string
	for i := 0; i < params.Len(); i++ {
		v := params.At(i)
		name := v.Name()
		if name == "" {
			name = fmt.Sprintf("param%v", i+1)
		}
		if i > 0 {
			info += ","
		}
		typ := v.Type().String()
		typ = re.ReplaceAllStringFunc(typ, func(s string) string {
			s = strings.Replace(s, ".", "\".", 1)
			return "@\"" + s
		})
		info += name + " " + typ
	}
	return info
}

func (p *types_parser) exportData() []byte {
	fset := token.NewFileSet()
	var buf bytes.Buffer
	gcexportdata.Write(&buf, fset, p.pkg)
	return buf.Bytes()
}

func (p *types_parser) export() []byte {
	var ar []string
	ar = append(ar, "package "+p.pkg.Name())

	ar = append(ar, `func @"".Expand(s string,mapping func(string) string)(param1 string)`)
	ar = append(ar, "\n")
	return []byte(strings.Join(ar, "\n"))

	scope := p.pkg.Scope()
	re, _ := regexp.Compile("[\\w]+\\.[\\w]+")
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		var info string
		switch typ := (obj).(type) {
		case *types.Func:
			sig := typ.Type().(*types.Signature)
			if sig.Recv() != nil {
				log.Fatalln(obj.String())
			}
			info = `func @"".` + obj.Name()
			info += "("
			if sig.Params() != nil {
				info += MakeTuple(re, sig.Params())
			}
			info += ")"
			if sig.Results() != nil {
				info += "("
				info += MakeTuple(re, sig.Results())
				info += ")"
			}
		case *types.Const:
			info = `const @"".` + obj.Name() + "=" + typ.Val().String()
		case *types.Var:
			info = strings.Replace(obj.String(), p.pkg.Name()+".", `@"".`, 1)
			info = re.ReplaceAllStringFunc(info, func(s string) string {
				s = strings.Replace(s, ".", "\".", 1)
				return "@\"" + s
			})
			info = strings.Replace(info, " untyped ", " ", -1)
			//str = "type @\"\".ScanState interface{Read(buf []byte) (n int, err error);SkipSpace()}"
			//str = "func @\"\".test(f func() bool))" //; Token(skipSpace bool, f func(rune) bool) (token []byte, err error); UnreadRune() error; Width() (wid int, ok bool)}"
		case *types.TypeName:
			//log.Println(typ.Type().Underlying())
			switch tt := typ.Type().Underlying().(type) {
			case *types.Interface:
				info = "type @\"\"." + obj.Name() + " interface{}"
			case *types.Struct:
				info = "type @\"\"." + obj.Name() + " struct{}"
			case *types.Basic:
				info = strings.Replace(obj.String(), p.pkg.Name()+".", `@"".`, 1)
				info = re.ReplaceAllStringFunc(info, func(s string) string {
					s = strings.Replace(s, ".", "\".", 1)
					return "@\"" + s
				})
			default:
				info = "type @\"\"." + obj.Name() + " " + typ.Type().String()
				log.Fatalf("TypeName %T\n", tt)
			}
		default:
			log.Fatalf("types %T\n", typ)
		}
		//info = `func @"".Create(name string)(param1 *@"os".File,param2 error)`
		info = `func @"".Expand(s string,mapping func(string) string)(param1 string)`
		log.Println(info)
		if info != "" {
			ar = append(ar, info)
		}
	}
	ar = append(ar, "\n")

	return []byte(strings.Join(ar, "\n"))
}

func (p *types_parser) parse_export(callback func(pkg string, decl ast.Decl)) {
	if p.pkg == nil {
		return
	}
	scope := p.pkg.Scope()
	var pname = "!" + p.pfc.name + "!" + p.pkg.Name()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		decl := objToAst(obj)
		if decl == nil {
			continue
		}
		callback(pname, decl)
	}
}
