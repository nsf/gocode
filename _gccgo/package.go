package main

import "debug/elf"
import "text/scanner"
import "bytes"
import "errors"
import "io"
import "fmt"
import "strconv"
import "go/ast"
import "go/token"
import "strings"

var builtinTypeNames = []*ast.Ident{
	nil,
	ast.NewIdent("int8"),
	ast.NewIdent("int16"),
	ast.NewIdent("int32"),
	ast.NewIdent("int64"),
	ast.NewIdent("uint8"),
	ast.NewIdent("uint16"),
	ast.NewIdent("uint32"),
	ast.NewIdent("uint64"),
	ast.NewIdent("float32"),
	ast.NewIdent("float64"),
	ast.NewIdent("int"),
	ast.NewIdent("uint"),
	ast.NewIdent("uintptr"),
	nil,
	ast.NewIdent("bool"),
	ast.NewIdent("string"),
	ast.NewIdent("complex64"),
	ast.NewIdent("complex128"),
	ast.NewIdent("error"),
	ast.NewIdent("byte"),
	ast.NewIdent("rune"),
}

const (
	smallestBuiltinCode = -21
)

func readImportData(importPath string) ([]byte, error) {
	// TODO: find file location
	filename := importPath + ".gox"

	f, err := elf.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sec := f.Section(".go_export")
	if sec == nil {
		return nil, errors.New("missing .go_export section in the file: " + filename)
	}

	return sec.Data()
}

func parseImportData(data []byte) {
	buf := bytes.NewBuffer(data)
	var p importDataParser
	p.init(buf)

	// magic
	p.expectIdent("v1")
	p.expect(';')

	// package ident
	p.expectIdent("package")
	pkgid := p.expect(scanner.Ident)
	p.expect(';')

	println("package ident: " + pkgid)

	// package path
	p.expectIdent("pkgpath")
	pkgpath := p.expect(scanner.Ident)
	p.expect(';')

	println("package path: " + pkgpath)

	// package priority
	p.expectIdent("priority")
	priority := p.expect(scanner.Int)
	p.expect(';')

	println("package priority: " + priority)

	// import init functions
	for p.toktype == scanner.Ident && p.token() == "import" {
		p.expectIdent("import")
		pkgname := p.expect(scanner.Ident)
		pkgpath := p.expect(scanner.Ident)
		importpath := p.expect(scanner.String)
		p.expect(';')
		println("import " + pkgname + " " + pkgpath + " " + importpath)
	}

	if p.toktype == scanner.Ident && p.token() == "init" {
		p.expectIdent("init")
		for p.toktype != ';' {
			pkgname := p.expect(scanner.Ident)
			initname := p.expect(scanner.Ident)
			prio := p.expect(scanner.Int)
			println("init " + pkgname + " " + initname + " " + fmt.Sprint(prio))
		}
		p.expect(';')
	}

loop:
	for {
		switch tok := p.expect(scanner.Ident); tok {
		case "const":
			p.readConst()
		case "type":
			p.readTypeDecl()
		case "var":
			p.readVar()
		case "func":
			p.readFunc()
		case "checksum":
			p.readChecksum()
			break loop
		default:
			panic(errors.New("unexpected identifier token: '" + tok + "'"))
		}
	}
}

//----------------------------------------------------------------------------
// import data parser
//----------------------------------------------------------------------------

type importDataType struct {
	name  string
	type_ ast.Expr
}

type importDataParser struct {
	scanner   scanner.Scanner
	toktype   rune
	typetable []*importDataType
}

func (this *importDataParser) init(reader io.Reader) {
	this.scanner.Mode = scanner.ScanIdents | scanner.ScanInts | scanner.ScanStrings | scanner.ScanFloats
	this.scanner.Init(reader)
	this.next()

	// len == 1 here, because 0 is an invalid type index
	this.typetable = make([]*importDataType, 1, 50)
}

func (this *importDataParser) next() {
	this.toktype = this.scanner.Scan()
}

func (this *importDataParser) token() string {
	return this.scanner.TokenText()
}

// internal, use expect(scanner.Ident) instead
func (this *importDataParser) readIdent() string {
	id := ""
	prev := rune(0)

loop:
	for {
		switch this.toktype {
		case scanner.Ident:
			if prev == scanner.Ident {
				break loop
			}

			prev = this.toktype
			id += this.token()
			this.next()
		case '.', '?', '$':
			prev = this.toktype
			id += string(this.toktype)
			this.next()
		default:
			break loop
		}
	}

	if id == "" {
		this.errorf("identifier expected, got %s", scanner.TokenString(this.toktype))
	}
	return id
}

func (this *importDataParser) readInt() string {
	val := ""
	if this.toktype == '-' {
		this.next()
		val += "-"
	}
	if this.toktype != scanner.Int {
		this.errorf("expected: %s, got: %s", scanner.TokenString(scanner.Int), scanner.TokenString(this.toktype))
	}

	val += this.token()
	this.next()
	return val
}

func (this *importDataParser) errorf(format string, args ...interface{}) {
	panic(errors.New(fmt.Sprintf(format, args...)))
}

// makes sure that the current token is 'x', returns it and reads the next one
func (this *importDataParser) expect(x rune) string {
	if x == scanner.Ident {
		// special case, in gccgo import data identifier is not exactly a scanner.Ident
		return this.readIdent()
	}

	if x == scanner.Int {
		// another special case, handle negative ints as well
		return this.readInt()
	}

	if this.toktype != x {
		this.errorf("expected: %s, got: %s", scanner.TokenString(x), scanner.TokenString(this.toktype))
	}

	tok := this.token()
	this.next()
	return tok
}

// makes sure that the following set of tokens matches 'special', reads the next one
func (this *importDataParser) expectSpecial(special string) {
	i := 0
	for i < len(special) {
		if this.toktype != rune(special[i]) {
			break
		}

		this.next()
		i++
	}

	if i < len(special) {
		this.errorf("expected: \"%s\", got something else", special)
	}
}

// makes sure that the current token is scanner.Ident and is equals to 'ident', reads the next one
func (this *importDataParser) expectIdent(ident string) {
	tok := this.expect(scanner.Ident)
	if tok != ident {
		this.errorf("expected identifier: \"%s\", got: \"%s\"", ident, tok)
	}
}

func (this *importDataParser) readType() ast.Expr {
	type_, name := this.readTypeFull()
	if name != "" {
		return ast.NewIdent(name)
	}
	return type_
}

func (this *importDataParser) readTypeFull() (ast.Expr, string) {
	this.expect('<')
	this.expectIdent("type")

	numstr := this.expect(scanner.Int)
	num, err := strconv.ParseInt(numstr, 10, 32)
	if err != nil {
		panic(err)
	}

	if this.toktype == '>' {
		// was already declared previously
		this.next()
		if num < 0 {
			if num < smallestBuiltinCode {
				this.errorf("out of range built-in type code")
			}
			return builtinTypeNames[-num], ""
		} else {
			// lookup type table
			type_ := this.typetable[num]
			return type_.type_, type_.name
		}
	}

	this.typetable = append(this.typetable, &importDataType{})
	var type_ = this.typetable[len(this.typetable)-1]

	switch this.toktype {
	case scanner.String:
		// named type
		s := this.expect(scanner.String)
		type_.name = s[1 : len(s)-1] // remove ""
		fallthrough
	default:
		// unnamed type
		switch this.toktype {
		case scanner.Ident:
			switch tok := this.token(); tok {
			case "struct":
				type_.type_ = this.readStructType()
			case "interface":
				type_.type_ = this.readInterfaceType()
			case "map":
				type_.type_ = this.readMapType()
			case "chan":
				type_.type_ = this.readChanType()
			default:
				this.errorf("unknown type class token: \"%s\"", tok)
			}
		case '[':
			type_.type_ = this.readArrayOrSliceType()
		case '*':
			this.next()
			if this.token() == "any" {
				this.next()
				type_.type_ = &ast.StarExpr{X: ast.NewIdent("any")}
			} else {
				type_.type_ = &ast.StarExpr{X: this.readType()}
			}
		case '(':
			type_.type_ = this.readFuncType()
		case '<':
			type_.type_ = this.readType()
		}
	}

	for this.toktype != '>' {
		// must be a method or many methods
		this.expectIdent("func")
		this.readMethod()
	}

	this.expect('>')
	return type_.type_, type_.name
}

func (this *importDataParser) readMapType() ast.Expr {
	this.expectIdent("map")
	this.expect('[')
	key := this.readType()
	this.expect(']')
	val := this.readType()
	return &ast.MapType{Key: key, Value: val}
}

func (this *importDataParser) readChanType() ast.Expr {
	dir := ast.SEND | ast.RECV
	this.expectIdent("chan")
	switch this.toktype {
	case '-':
		// chan -< <type>
		this.expectSpecial("-<")
		dir = ast.SEND
	case '<':
		// slight ambiguity here
		if this.scanner.Peek() == '-' {
			// chan <- <type>
			this.expectSpecial("<-")
			dir = ast.RECV
		}
		// chan <type>
	default:
		this.errorf("unexpected token: \"%s\"", this.token())
	}

	return &ast.ChanType{Dir: dir, Value: this.readType()}
}

func (this *importDataParser) readField() *ast.Field {
	var tag string
	name := this.expect(scanner.Ident)
	type_ := this.readType()
	if this.toktype == scanner.String {
		tag = this.expect(scanner.String)
	}

	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  type_,
		Tag:   &ast.BasicLit{Kind: token.STRING, Value: tag},
	}
}

func (this *importDataParser) readStructType() ast.Expr {
	var fields []*ast.Field
	readField := func() {
		field := this.readField()
		fields = append(fields, field)
	}

	this.expectIdent("struct")
	this.expect('{')
	for this.toktype != '}' {
		readField()
		this.expect(';')
	}
	this.expect('}')
	return &ast.StructType{Fields: &ast.FieldList{List: fields}}
}

func (this *importDataParser) readParameter() *ast.Field {
	name := this.expect(scanner.Ident)

	var type_ ast.Expr
	if this.toktype == '.' {
		this.expectSpecial("...")
		type_ = &ast.Ellipsis{Elt: this.readType()}
	} else {
		type_ = this.readType()
	}

	var tag string
	if this.toktype == scanner.String {
		tag = this.expect(scanner.String)
	}

	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  type_,
		Tag:   &ast.BasicLit{Kind: token.STRING, Value: tag},
	}
}

func (this *importDataParser) readParameters() *ast.FieldList {
	var fields []*ast.Field
	readParameter := func() {
		parameter := this.readParameter()
		fields = append(fields, parameter)
	}

	this.expect('(')
	if this.toktype != ')' {
		readParameter()
		for this.toktype == ',' {
			this.next() // skip ','
			readParameter()
		}
	}
	this.expect(')')

	if fields == nil {
		return nil
	}
	return &ast.FieldList{List: fields}
}

func (this *importDataParser) readFuncType() *ast.FuncType {
	var params, results *ast.FieldList

	params = this.readParameters()
	switch this.toktype {
	case '<':
		field := &ast.Field{Type: this.readType()}
		results = &ast.FieldList{List: []*ast.Field{field}}
	case '(':
		results = this.readParameters()
	}

	return &ast.FuncType{Params: params, Results: results}
}

func (this *importDataParser) readMethodOrEmbedSpec() *ast.Field {
	var type_ ast.Expr
	name := this.expect(scanner.Ident)
	if name == "?" {
		// TODO: ast.SelectorExpr conversion here possibly
		type_ = this.readType()
	} else {
		type_ = this.readFuncType()
	}
	return &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(name)},
		Type:  type_,
	}
}

func (this *importDataParser) readInterfaceType() ast.Expr {
	var methods []*ast.Field
	readMethod := func() {
		method := this.readMethodOrEmbedSpec()
		methods = append(methods, method)
	}

	this.expectIdent("interface")
	this.expect('{')
	for this.toktype != '}' {
		readMethod()
		this.expect(';')
	}
	this.expect('}')
	return &ast.InterfaceType{Methods: &ast.FieldList{List: methods}}
}

func (this *importDataParser) readMethod() {
	var buf1, buf2 bytes.Buffer
	recv := this.readParameters()
	name := this.expect(scanner.Ident)
	type_ := this.readFuncType()
	this.expect(';')
	prettyPrintTypeExpr(&buf1, recv.List[0].Type)
	prettyPrintTypeExpr(&buf2, type_)
	println("func (" + buf1.String() + ") " + name + buf2.String()[4:])
}

func (this *importDataParser) readArrayOrSliceType() ast.Expr {
	var length ast.Expr

	this.expect('[')
	if this.toktype == scanner.Int {
		// array type
		length = &ast.BasicLit{Kind: token.INT, Value: this.expect(scanner.Int)}
	}
	this.expect(']')
	return &ast.ArrayType{
		Len: length,
		Elt: this.readType(),
	}
}

func (this *importDataParser) readConst() {
	var buf bytes.Buffer

	// const keyword was already consumed
	c := "const " + this.expect(scanner.Ident)
	if this.toktype != '=' {
		// parse type
		type_ := this.readType()
		prettyPrintTypeExpr(&buf, type_)
		c += " " + buf.String()
	}

	this.expect('=')

	// parse expr
	this.next()
	this.expect(';')
	println(c)
}

func (this *importDataParser) readChecksum() {
	// checksum keyword was already consumed
	for this.toktype != ';' {
		this.next()
	}
	this.expect(';')
}

func (this *importDataParser) readTypeDecl() {
	var buf bytes.Buffer
	// type keyword was already consumed
	type_, name := this.readTypeFull()
	this.expect(';')
	prettyPrintTypeExpr(&buf, type_)
	println("type " + name + " " + buf.String())
}

func (this *importDataParser) readVar() {
	var buf bytes.Buffer
	// var keyword was already consumed
	name := this.expect(scanner.Ident)
	type_ := this.readType()
	this.expect(';')
	prettyPrintTypeExpr(&buf, type_)
	println("var " + name + " " + buf.String())
}

func (this *importDataParser) readFunc() {
	var buf bytes.Buffer
	// func keyword was already consumed
	name := this.expect(scanner.Ident)
	type_ := this.readFuncType()
	this.expect(';')
	prettyPrintTypeExpr(&buf, type_)
	println("func " + name + buf.String()[4:])
}

//-------------------------------------------------------------------------
// Pretty printing
//-------------------------------------------------------------------------

func getArrayLen(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.BasicLit:
		return string(t.Value)
	case *ast.Ellipsis:
		return "..."
	}
	return ""
}

func prettyPrintTypeExpr(out io.Writer, e ast.Expr) {
	switch t := e.(type) {
	case *ast.StarExpr:
		fmt.Fprintf(out, "*")
		prettyPrintTypeExpr(out, t.X)
	case *ast.Ident:
		if strings.HasPrefix(t.Name, "$") {
			// beautify anonymous types
			switch t.Name[1] {
			case 's':
				fmt.Fprintf(out, "struct")
			case 'i':
				fmt.Fprintf(out, "interface")
			}
		} else {
			fmt.Fprintf(out, t.Name)
		}
	case *ast.ArrayType:
		al := ""
		if t.Len != nil {
			println(t.Len)
			al = getArrayLen(t.Len)
		}
		if al != "" {
			fmt.Fprintf(out, "[%s]", al)
		} else {
			fmt.Fprintf(out, "[]")
		}
		prettyPrintTypeExpr(out, t.Elt)
	case *ast.SelectorExpr:
		prettyPrintTypeExpr(out, t.X)
		fmt.Fprintf(out, ".%s", t.Sel.Name)
	case *ast.FuncType:
		fmt.Fprintf(out, "func(")
		prettyPrintFuncFieldList(out, t.Params)
		fmt.Fprintf(out, ")")

		buf := bytes.NewBuffer(make([]byte, 0, 256))
		nresults := prettyPrintFuncFieldList(buf, t.Results)
		if nresults > 0 {
			results := buf.String()
			if strings.Index(results, ",") != -1 {
				results = "(" + results + ")"
			}
			fmt.Fprintf(out, " %s", results)
		}
	case *ast.MapType:
		fmt.Fprintf(out, "map[")
		prettyPrintTypeExpr(out, t.Key)
		fmt.Fprintf(out, "]")
		prettyPrintTypeExpr(out, t.Value)
	case *ast.InterfaceType:
		fmt.Fprintf(out, "interface{}")
	case *ast.Ellipsis:
		fmt.Fprintf(out, "...")
		prettyPrintTypeExpr(out, t.Elt)
	case *ast.StructType:
		fmt.Fprintf(out, "struct")
	case *ast.ChanType:
		switch t.Dir {
		case ast.RECV:
			fmt.Fprintf(out, "<-chan ")
		case ast.SEND:
			fmt.Fprintf(out, "chan<- ")
		case ast.SEND | ast.RECV:
			fmt.Fprintf(out, "chan ")
		}
		prettyPrintTypeExpr(out, t.Value)
	case *ast.ParenExpr:
		fmt.Fprintf(out, "(")
		prettyPrintTypeExpr(out, t.X)
		fmt.Fprintf(out, ")")
	case *ast.BadExpr:
		// TODO: probably I should check that in a separate function
		// and simply discard declarations with BadExpr as a part of their
		// type
	default:
		// should never happen
		panic("unknown type")
	}
}

func prettyPrintFuncFieldList(out io.Writer, f *ast.FieldList) int {
	count := 0
	if f == nil {
		return count
	}
	for i, field := range f.List {
		// names
		if field.Names != nil {
			hasNonblank := false
			for j, name := range field.Names {
				if name.Name != "?" {
					hasNonblank = true
					fmt.Fprintf(out, "%s", name.Name)
					if j != len(field.Names)-1 {
						fmt.Fprintf(out, ", ")
					}
				}
				count++
			}
			if hasNonblank {
				fmt.Fprintf(out, " ")
			}
		} else {
			count++
		}

		// type
		prettyPrintTypeExpr(out, field.Type)

		// ,
		if i != len(f.List)-1 {
			fmt.Fprintf(out, ", ")
		}
	}
	return count
}

func main() {
	data, err := readImportData("io")
	if err != nil {
		panic(err)
	}
	parseImportData(data)
}
