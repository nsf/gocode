package main

import (
	"os"
	"fmt"
)

var Config = struct {
	ProposeBuiltins bool "propose-builtins"
}{
	false,
}

func parseAsync(file string, done chan *ModuleCache) {
	go func() {
		m := NewModuleCache("__this__", file)
		m.updateCache()
		done <- m
	}()
}

func main() {
	done := make(chan *ModuleCache)
	for _, arg := range os.Args[1:] {
		parseAsync(arg, done)
	}
	for _, _ = range os.Args[1:] {
		d := <-done
		fmt.Printf("%s was parsed successfully\n", d.filename)
		fmt.Printf("\t%d main declaration(s)\n", len(d.main.Children))
		fmt.Printf("\t%d foreign module(s)\n", len(d.others))
	}
	fmt.Printf("Total number of packages: %d\nOK\n", len(os.Args[1:]))
}
