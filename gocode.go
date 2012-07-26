package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

var (
	g_is_server = flag.Bool("s", false, "run a server instead of a client")
	g_format    = flag.String("f", "nice", "output format (vim | emacs | nice | csv | json)")
	g_input     = flag.String("in", "", "use this file instead of stdin input")
	g_sock      = create_sock_flag("sock", "socket type (unix | tcp)")
	g_addr      = flag.String("addr", "localhost:37373", "address for tcp socket")
)

func get_socket_filename() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "all"
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("gocode-daemon.%s", user))
}

func show_usage() {
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
	flag.Usage = show_usage
	flag.Parse()

	var retval int
	if *g_is_server {
		retval = do_server()
	} else {
		retval = do_client()
	}
	os.Exit(retval)
}
