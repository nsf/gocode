package main

import (
	"flag"
	"os/signal"
	"os"
	"path/filepath"
	"exec"
)

func CreateSockFlag(name, desc string) *string {
	return flag.String(name, "unix", desc)
}

func IsTerminationSignal(sig signal.Signal) bool {
	usig, ok := sig.(signal.UnixSignal)
	if !ok {
		return false
	}
	if usig == signal.SIGINT || usig == signal.SIGTERM {
		return true
	}
	return false
}

// Full path of the current executable
func GetExecutableFileName() string {
	// try readlink first
	path, err := os.Readlink("/proc/self/exe")
	if err == nil {
		return path
	}
	// use argv[0]
	path = os.Args[0]
	if !filepath.IsAbs(path) {
		cwd, _ := os.Getwd()
		path = filepath.Join(cwd, path)
	}
	if fileExists(path) {
		return path
	}
	// Fallback : use "gocode" and assume we are in the PATH...
	path, err = exec.LookPath("gocode")
	if err == nil {
		return path
	}
	return ""
}
