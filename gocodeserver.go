package main

import (
	"net"
	"rpc"
	"os/signal"
)

//-------------------------------------------------------------------------

type AutoCompletionDaemon struct {
	acr *ACRServer
	ctx *AutoCompleteContext
}

func NewAutoCompletionDaemon(path string) *AutoCompletionDaemon {
	self := new(AutoCompletionDaemon)
	self.acr = NewACRServer(path)
	self.ctx = NewAutoCompleteContext()
	return self
}

var daemon *AutoCompletionDaemon

//-------------------------------------------------------------------------

func Server_AutoComplete(file []byte, apropos string) ([]string, []string) {
	return daemon.ctx.Apropos(file, apropos)
}

func Server_Close(notused int) int {
	daemon.acr.Close()
	return 0
}

func Server_Status(notused int) string {
	return daemon.ctx.Status()
}

//-------------------------------------------------------------------------
// Autocompletion Refactoring Server
//-------------------------------------------------------------------------

const (
	ACR_CLOSE = iota
)

type ACRServer struct {
	listener *net.UnixListener
	cmd_in chan int
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
