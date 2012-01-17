// +build darwin freebsd linux netbsd openbsd
package main

import (
	"flag"
	"os"
	"os/signal"
	"os/exec"
	"path/filepath"
	"net"
	"net/rpc"
	"runtime"
)

func CreateSockFlag(name, desc string) *string {
	return flag.String(name, "unix", desc)
}

func IsTerminationSignal(sig os.Signal) bool {
	usig, ok := sig.(os.UnixSignal)
	if !ok {
		return false
	}
	if usig == os.SIGINT || usig == os.SIGTERM {
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

func (s *Server) Loop() {
	conn_in := make(chan net.Conn)
	go acceptConnections(conn_in, s.listener)
	for {
		// handle connections or server CMDs (currently one CMD)
		select {
		case c := <-conn_in:
			rpc.ServeConn(c)
			runtime.GC()
		case cmd := <-s.cmd_in:
			switch cmd {
			case SERVER_CLOSE:
				return
			}
		case sig := <-signal.Incoming:
			if IsTerminationSignal(sig) {
				return
			}
		}
	}
}

