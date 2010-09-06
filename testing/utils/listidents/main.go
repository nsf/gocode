package main

import (
	"go/scanner"
	"go/token"
	"io/ioutil"
	"json"
	"os"
	"fmt"
)

type Ident struct {
	Name string
	Offset int
}

var idents = make([]Ident, 0, 16)

func appendIdent(name string, offset int) {
	n := len(idents)
	if cap(idents) < n+1 {
		s := make([]Ident, n, n*2+1)
		copy(s, idents)
		idents = s
	}

	idents = idents[0 : n+1]
	idents[n] = Ident{name, offset}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: listidents <filename>")
		return
	}

	data, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		panic(err.String())
	}

	var s scanner.Scanner
	s.Init(os.Args[1], data, nil, 0)

	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}

		if tok == token.PACKAGE {
			// skip package name
			s.Scan()
			continue
		}

		if tok == token.IDENT {
			if lit[0] == '_' {
				// skip empty variables
				continue
			}
			appendIdent(string(lit), pos.Offset)
		}
	}

	data, err = json.Marshal(idents)
	if err != nil {
		panic(err.String())
	}
	os.Stdout.Write(data)
}
