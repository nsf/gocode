package main

import (
	"os"
	"fmt"
)

func main() {
	ctx := NewAutoCompleteContext()
	ctx.debuglog = os.Stdout
	for _, arg := range os.Args[1:] {
		ctx.current.processPackage(arg, "__this__", "")
	}
	fmt.Printf("Total number of packages: %d\nOK\n", len(os.Args[1:]))
}
