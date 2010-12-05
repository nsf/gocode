package main

import (
	"flag"
	"os/signal"
)

func CreateSockFlag(name, desc string) *string {
	return flag.String(name, "tcp", desc)
}

func IsTerminationSignal(sig signal.Signal) bool {
	return false
}

func IsAbsPath(p string) bool {
	if p[0] == '\\' || (len(p) > 1 && p[1] == ':') {
		return true
	}
	return false
}
