package main

import (
	"net"
	"rpc/jsonrpc"
	"fmt"
	"os"
	"io"
)

//-------------------------------------------------------------------------
// ACR type (used in RPC)
//-------------------------------------------------------------------------

type ACR struct {
	acr *ACRServer
}

// shutdown request

func (self *ACR) Shutdown(notused1 *int, notused2 *int) os.Error {
	self.acr.Close()
	return nil
}

// top-level imported module autocomplete request

type ACRImportedAC struct {
	file string
	apropos string
}

type ACRImportedACReply struct {
	completions []string
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
	self.cmd_in = make(chan int)
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

type RPCDebugProxy struct {
	rwc io.ReadWriteCloser
	debugout io.Writer
}

func (self *RPCDebugProxy) Read(p []byte) (n int, err os.Error) {
	tmp := make([]byte, len(p))
	n, err = self.rwc.Read(tmp)
	copy(p, tmp)
	fmt.Fprintf(self.debugout, "Request:\n")
	self.debugout.Write(tmp)
	return
}

func (self *RPCDebugProxy) Write(p []byte) (n int, err os.Error) {
	n, err = self.rwc.Write(p)
	fmt.Fprintf(self.debugout, "Response:\n")
	self.debugout.Write(p)
	return
}

func (self *RPCDebugProxy) Close() os.Error {
	return self.rwc.Close()
}

func NewRPCDebugProxy(rwc io.ReadWriteCloser, debugout io.Writer) *RPCDebugProxy {
	self := new(RPCDebugProxy)
	self.rwc = rwc
	self.debugout = debugout
	return self
}

func (self *ACRServer) Loop() {
	conn_in := make(chan net.Conn)
	go acceptConnections(conn_in, self.listener)
	for {
		// handle connections or server CMDs (currently one CMD)
		select {
		case c := <-conn_in:
			go func(c net.Conn) {
				jsonrpc.ServeConn(NewRPCDebugProxy(c, os.Stdout))
			}(c)
		case cmd := <-self.cmd_in:
			switch cmd {
			case ACR_CLOSE:
				return
			}
		}
	}
}

func (self *ACRServer) Close() {
	self.cmd_in <- ACR_CLOSE
}
