package main

import (
	"net"
	"rpc"
	"os/signal"
	"fmt"
	"runtime"
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
	self := new(AutoCompletionDaemon)
	self.acr = NewACRServer(path)
	self.mcache = NewMCache()
	self.declcache = NewDeclCache()
	self.acc = NewAutoCompleteContext(self.mcache, self.declcache)
	self.semantic = NewSemanticContext(self.mcache, self.declcache)
	return self
}

func (self *AutoCompletionDaemon) DropCache() {
	self.mcache = NewMCache()
	self.declcache = NewDeclCache()
	self.acc = NewAutoCompleteContext(self.mcache, self.declcache)
	self.semantic = NewSemanticContext(self.mcache, self.declcache)
}

var daemon *AutoCompletionDaemon

//-------------------------------------------------------------------------

func printBacktrace() {
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
}

func Server_AutoComplete(file []byte, filename string, cursor int) (a, b, c []string, d int) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("GOT PANIC!!!:\n")
			fmt.Println(err)
			printBacktrace()
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
			fmt.Printf("GOT PANIC!!!:\n")
			fmt.Println(err)
			printBacktrace()

			// drop cache
			daemon.DropCache()
		}
	}()
	return daemon.semantic.GetSMap(filename)
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
	self := new(ACRServer)
	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		panic(err.String())
	}

	self.listener, err = net.ListenUnix("unix", addr)
	if err != nil {
		panic(err.String())
	}
	self.cmd_in = make(chan int, 1)
	return self
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

func (self *ACRServer) Loop() {
	conn_in := make(chan net.Conn)
	go acceptConnections(conn_in, self.listener)
	for {
		// handle connections or server CMDs (currently one CMD)
		select {
		case c := <-conn_in:
			rpc.ServeConn(c)
			runtime.GC()
		case cmd := <-self.cmd_in:
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

func (self *ACRServer) Close() {
	self.cmd_in <- ACR_CLOSE
}
