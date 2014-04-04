package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

var (
	gIsServer = flag.Bool("s", false, "run a server instead of a client")
	gFormat    = flag.String("f", "nice", "output format (vim | emacs | nice | csv | json)")
	gInput     = flag.String("in", "", "use this file instead of stdin input")
	gSock      = createSockFlag("sock", "socket type (unix | tcp)")
	gAddr      = flag.String("addr", "localhost:37373", "address for tcp socket")
	gDebug     = flag.Bool("debug", false, "enable server-side debug mode")
)

func getSocketFilename() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "all"
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("gocode-daemon.%s", user))
}

func showUsage() {
	fmt.Fprintf(os.Stderr,
		"Usage: %s [-s] [-f=<format>] [-in=<path>] [-sock=<type>] [-addr=<addr>]\n"+
			"       <command> [<args>]\n\n",
		os.Args[0])
	fmt.Fprintf(os.Stderr,
		"Flags:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr,
		"\nCommands:\n"+
			"  autocomplete [<path>] <offset>     main autocompletion command\n"+
			"  close                              close the gocode daemon\n"+
			"  status                             gocode daemon status report\n"+
			"  drop-cache                         drop gocode daemon's cache\n"+
			"  set [<name> [<value>]]             list or set config options\n")
}

func main() {
	flag.Usage = showUsage
	flag.Parse()

	var retval int
	if *gIsServer {
		retval = doServer()
	} else {
		retval = doClient()
	}
	os.Exit(retval)
}
