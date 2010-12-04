package main

import (
	"flag"
	"os/signal"
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
