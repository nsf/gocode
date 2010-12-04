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
