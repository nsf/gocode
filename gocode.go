package main

import (
//	"os"
//	"io/ioutil"
//	"strings"
	"rpc"
	"rpc/jsonrpc"
	"flag"
	"fmt"
	"os"
)

var (
	server = flag.Bool("s", false, "run a server instead of a client")
)

func serverFunc() {
	acrRPC := new(ACR)
	rpc.Register(acrRPC)
	acrRPC.acr = NewACRServer("/tmp/acrserver")
	defer os.Remove("/tmp/acrserver")
	acrRPC.acr.Loop()
}

func clientFunc() {
	// client
	var args, reply int

	client, err := jsonrpc.Dial("unix", "/tmp/acrserver")
	if err != nil {
		panic(err.String())
	}
	err = client.Call("ACR.Shutdown", &args, &reply)
	if err != nil {
		panic(err.String())
	}
	fmt.Printf("close request send\n")
}

func main() {
	flag.Parse()
	if *server {
		serverFunc()
	} else {
		clientFunc()
	}
	/*
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
	*/
}
