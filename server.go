package main

import (
	"net"
	"rpc"
	"os/signal"
	"fmt"
	"runtime"
	"sync"
)

//-------------------------------------------------------------------------

type AutoCompletionDaemon struct {
	acr *ACRServer
	acc *AutoCompleteContext
	semantic *SemanticContext
	mcache MCache
	declcache *DeclCache
}

func NewAutoCompletionDaemon(path string) *AutoCompletionDaemon {
	d := new(AutoCompletionDaemon)
	d.acr = NewACRServer(path)
	d.mcache = NewMCache()
	d.declcache = NewDeclCache()
	d.acc = NewAutoCompleteContext(d.mcache, d.declcache)
	d.semantic = NewSemanticContext(d.mcache, d.declcache)
	return d
}

func (d *AutoCompletionDaemon) DropCache() {
	d.mcache = NewMCache()
	d.declcache = NewDeclCache()
	d.acc = NewAutoCompleteContext(d.mcache, d.declcache)
	d.semantic = NewSemanticContext(d.mcache, d.declcache)
}

var daemon *AutoCompletionDaemon

//-------------------------------------------------------------------------

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

func Server_AutoComplete(file []byte, filename string, cursor int) (a, b, c []string, d int) {
	defer func() {
		if err := recover(); err != nil {
			printBacktrace(err)
			a = []string{"PANIC"}
			b = a
			c = a

			// drop cache
			daemon.DropCache()
		}
	}()
	a, b, c, d = daemon.acc.Apropos(file, filename, cursor)
	return
}

func Server_SMap(filename string) []DeclDesc {
	defer func() {
		if err := recover(); err != nil {
			printBacktrace(err)

			// drop cache
			daemon.DropCache()
		}
	}()
	return daemon.semantic.GetSMap(filename)
}

func Server_Rename(filename string, cursor int) []RenameDesc {
	defer func() {
		if err := recover(); err != nil {
			printBacktrace(err)

			// drop cache
			daemon.DropCache()
		}
	}()
	return daemon.semantic.Rename(filename, cursor)
}

func Server_Close(notused int) int {
	daemon.acr.Close()
	return 0
}

func Server_Status(notused int) string {
	return daemon.acc.Status()
}

func Server_DropCache(notused int) int {
	// drop cache
	daemon.DropCache()
	return 0
}

func Server_Set(key, value string) string {
	if key == "" {
		return listConfig(&Config)
	} else if value == "" {
		return listOption(&Config, key)
	}
	return setOption(&Config, key, value)
}

//-------------------------------------------------------------------------
// Autocompletion Refactoring Server
//-------------------------------------------------------------------------

const (
	ACR_CLOSE = iota
)

type ACRServer struct {
	listener *net.UnixListener
	cmd_in   chan int
}

func NewACRServer(path string) *ACRServer {
	s := new(ACRServer)
	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		panic(err.String())
	}

	s.listener, err = net.ListenUnix("unix", addr)
	if err != nil {
		panic(err.String())
	}
	s.cmd_in = make(chan int, 1)
	return s
}

func acceptConnections(in chan net.Conn, listener *net.UnixListener) {
	for {
		c, err := listener.Accept()
		if err != nil {
			panic(err.String())
		}
		in <- c
	}
}

func (s *ACRServer) Loop() {
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
			case ACR_CLOSE:
				return
			}
		case sig := <-signal.Incoming:
			usig, ok := sig.(signal.UnixSignal)
			if !ok {
				break
			}
			if usig == signal.SIGINT || usig == signal.SIGTERM {
				return
			}
		}
	}
}

func (s *ACRServer) Close() {
	s.cmd_in <- ACR_CLOSE
}
