package main

import (
	"os"
	"fmt"
	"sync"
	"runtime"
)

var Config = struct {
	ProposeBuiltins bool "propose-builtins"
	DenyModuleRenames bool "deny-module-renames"
	LibPath string "lib-path"
}{
	false,
	false,
	"",
}

var btSync sync.Mutex

func printBacktrace(err interface{}) {
	btSync.Lock()
	defer btSync.Unlock()
	fmt.Printf("panic: %v\n", err)
	i := 2
	for {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		f := runtime.FuncForPC(pc)
		fmt.Printf("%d(%s): %s:%d\n", i-1, f.Name(), file, line)
		i++
	}
	fmt.Println("")
}

func parseAsync(file string, done chan *ModuleCache) {
	go func() {
		m := NewModuleCache(file)
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
		fmt.Printf("%s was parsed successfully\n", d.name)
		fmt.Printf("\t%d main declaration(s)\n", len(d.main.Children))
		fmt.Printf("\t%d foreign module(s)\n", len(d.others))
	}
	fmt.Printf("Total number of packages: %d\nOK\n", len(os.Args[1:]))
}
