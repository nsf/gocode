package main

import (
	"net"
	"fmt"
)

type ACRServer struct {
	listener *net.UnixListener
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
	return self
}

func (self *ACRServer) Loop() {
	for {
		c, err := self.listener.Accept()
		if err != nil {
			panic(err.String())
		}

		go func(c net.Conn) {
			fmt.Fprintf(c, "Preved!\n")
			c.Close()
		}(c)
	}
}

func (self *ACRServer) Close() {
	self.listener.Close()
}
