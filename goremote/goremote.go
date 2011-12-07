package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"reflect"
	"strings"
)

const prefix = "Server_"

func prettyPrintTypeExpr(out io.Writer, e ast.Expr) {
	ty := reflect.TypeOf(e)
	switch t := e.(type) {
	case *ast.StarExpr:
		fmt.Fprintf(out, "*")
		prettyPrintTypeExpr(out, t.X)
	case *ast.Ident:
		fmt.Fprintf(out, t.Name)
	case *ast.ArrayType:
		fmt.Fprintf(out, "[]")
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
			if strings.Index(results, " ") != -1 {
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
	default:
		fmt.Fprintf(out, "\n[!!] unknown type: %s\n", ty.String())
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
			for j, name := range field.Names {
				fmt.Fprintf(out, "%s", name.Name)
				if j != len(field.Names)-1 {
					fmt.Fprintf(out, ", ")
				}
				count++
			}
			fmt.Fprintf(out, " ")
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

func prettyPrintFuncFieldListUsingArgs(out io.Writer, f *ast.FieldList) int {
	count := 0
	if f == nil {
		return count
	}
	for i, field := range f.List {
		// names
		if field.Names != nil {
			for j, _ := range field.Names {
				fmt.Fprintf(out, "Arg%d", count)
				if j != len(field.Names)-1 {
					fmt.Fprintf(out, ", ")
				}
				count++
			}
			fmt.Fprintf(out, " ")
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

func generateStructWrapper(out io.Writer, fun *ast.FieldList, structname, name string) int {
	fmt.Fprintf(out, "type %s_%s struct {\n", structname, name)
	argn := 0
	for _, field := range fun.List {
		fmt.Fprintf(out, "\t")
		// names
		if field.Names != nil {
			for j, _ := range field.Names {
				fmt.Fprintf(out, "Arg%d", argn)
				if j != len(field.Names)-1 {
					fmt.Fprintf(out, ", ")
				}
				argn++
			}
			fmt.Fprintf(out, " ")
		} else {
			fmt.Fprintf(out, "Arg%d ", argn)
			argn++
		}

		// type
		prettyPrintTypeExpr(out, field.Type)

		// \n
		fmt.Fprintf(out, "\n")
	}
	fmt.Fprintf(out, "}\n")
	return argn
}

// function that is being exposed to an RPC API, but calls simple "Server_" one
func generateServerRPCWrapper(out io.Writer, fun *ast.FuncDecl, name string, argcnt, replycnt int) {
	fmt.Fprintf(out, "func (r *RPCRemote) RPCServer_%s(args *Args_%s, reply *Reply_%s) error {\n",
		name, name, name)

	fmt.Fprintf(out, "\t")
	for i := 0; i < replycnt; i++ {
		fmt.Fprintf(out, "reply.Arg%d", i)
		if i != replycnt-1 {
			fmt.Fprintf(out, ", ")
		}
	}
	fmt.Fprintf(out, " = %s(", fun.Name.Name)
	for i := 0; i < argcnt; i++ {
		fmt.Fprintf(out, "args.Arg%d", i)
		if i != argcnt-1 {
			fmt.Fprintf(out, ", ")
		}
	}
	fmt.Fprintf(out, ")\n")
	fmt.Fprintf(out, "\treturn nil\n}\n")
}

func generateClientRPCWrapper(out io.Writer, fun *ast.FuncDecl, name string, argcnt, replycnt int) {
	fmt.Fprintf(out, "func Client_%s(cli *rpc.Client, ", name)
	prettyPrintFuncFieldListUsingArgs(out, fun.Type.Params)
	fmt.Fprintf(out, ")")

	buf := bytes.NewBuffer(make([]byte, 0, 256))
	nresults := prettyPrintFuncFieldList(buf, fun.Type.Results)
	if nresults > 0 {
		results := buf.String()
		if strings.Index(results, " ") != -1 {
			results = "(" + results + ")"
		}
		fmt.Fprintf(out, " %s", results)
	}
	fmt.Fprintf(out, " {\n")
	fmt.Fprintf(out, "\tvar args Args_%s\n", name)
	fmt.Fprintf(out, "\tvar reply Reply_%s\n", name)
	for i := 0; i < argcnt; i++ {
		fmt.Fprintf(out, "\targs.Arg%d = Arg%d\n", i, i)
	}
	fmt.Fprintf(out, "\terr := cli.Call(\"RPCRemote.RPCServer_%s\", &args, &reply)\n", name)
	fmt.Fprintf(out, "\tif err != nil {\n")
	fmt.Fprintf(out, "\t\tpanic(err)\n\t}\n")

	fmt.Fprintf(out, "\treturn ")
	for i := 0; i < replycnt; i++ {
		fmt.Fprintf(out, "reply.Arg%d", i)
		if i != replycnt-1 {
			fmt.Fprintf(out, ", ")
		}
	}
	fmt.Fprintf(out, "\n}\n")
}

func wrapFunction(out io.Writer, fun *ast.FuncDecl) {
	name := fun.Name.Name[len(prefix):]
	fmt.Fprintf(out, "// wrapper for: %s\n\n", fun.Name.Name)
	argcnt := generateStructWrapper(out, fun.Type.Params, "Args", name)
	replycnt := generateStructWrapper(out, fun.Type.Results, "Reply", name)
	generateServerRPCWrapper(out, fun, name, argcnt, replycnt)
	generateClientRPCWrapper(out, fun, name, argcnt, replycnt)
	fmt.Fprintf(out, "\n")
}

func processFile(out io.Writer, filename string) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, 0)
	if err != nil {
		panic(err)
	}

	for _, decl := range file.Decls {
		if fdecl, ok := decl.(*ast.FuncDecl); ok {
			namelen := len(fdecl.Name.Name)
			if namelen >= len(prefix) && fdecl.Name.Name[0:len(prefix)] == prefix {
				wrapFunction(out, fdecl)
			}
		}
	}
}

const head = `// WARNING! Autogenerated by goremote, don't touch.

package main

import (
	"net/rpc"
)

type RPCRemote struct {
}

`

func main() {
	flag.Parse()
	fmt.Fprintf(os.Stdout, head)
	for _, file := range flag.Args() {
		processFile(os.Stdout, file)
	}
}
