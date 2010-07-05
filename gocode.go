package main

import (
	"os"
	"io/ioutil"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: ./gocode <apropos request>")
	}

	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		panic("Bad stdin")
	}
	ctx := NewAutoCompleteContext()
	ctx.processData(data)

	request := os.Args[1]
	parts := strings.Split(request, ".", 2)
	res := ""
	switch len(parts) {
	case 1:
		for _, decl := range ctx.m[ctx.cfns[request]] {
			prettyPrintDecl(os.Stdout, decl, "")
		}
	case 2:
		for _, decl := range ctx.m[ctx.cfns[parts[0]]] {
			prettyPrintDecl(os.Stdout, decl, parts[1])
		}
	}
	print(res)
}
