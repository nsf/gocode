package main

import (
	"io/ioutil"
	"strconv"
	"rpc"
	"flag"
	"fmt"
	"os"
)

var (
	server = flag.Bool("s", false, "run a server instead of a client")
	format = flag.String("f", "vim", "output format (currently only 'vim' is valid)")
)

func getSocketFilename() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "all"
	}
	return fmt.Sprintf("%s/acrserver.%s", os.TempDir(), user)
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return true
}

func serverFunc() int {
	socketfname := getSocketFilename()
	if fileExists(socketfname) {
		fmt.Printf("unix socket: '%s' already exists\n", socketfname)
		return 1
	}
	daemon = NewAutoCompletionDaemon(socketfname)
	defer os.Remove(socketfname)

	rpcremote := new(RPCRemote)
	rpc.Register(rpcremote)

	daemon.acr.Loop()
	return 0
}

func Cmd_Status(c *rpc.Client) {
	fmt.Printf("%s\n", Client_Status(c, 0))
}

func Cmd_AutoComplete(c *rpc.Client) {
	file, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		panic(err.String())
	}

	apropos := flag.Arg(1)
	if apropos == "_" {
		// XXX: tmp probably
		apropos = ""
	}

	cursor := -1
	if flag.NArg() > 1 {
		cursor, _ = strconv.Atoi(flag.Arg(2))
	}
	abbrs, words := Client_AutoComplete(c, file, apropos, cursor)
	if words == nil {
		fmt.Print("[]")
		return
	}

	if len(words) != len(abbrs) {
		panic("Lengths should match!")
	}

	fmt.Printf("[")
	for i := 0; i < len(words); i++ {
		fmt.Printf("{'word': '%s', 'abbr': '%s'}", words[i], abbrs[i])
		if i != len(words)-1 {
			fmt.Printf(", ")
		}
	}
	fmt.Printf("]")
}

func Cmd_Close(c *rpc.Client) {
	Client_Close(c, 0)
}

func clientFunc() int {
	// client

	client, err := rpc.Dial("unix", getSocketFilename())
	if err != nil {
		fmt.Printf("Failed to connect to the ACR server\n%s\n", err.String())
		return 1
	}
	defer client.Close()

	if flag.NArg() > 0 {
		switch flag.Arg(0) {
		case "autocomplete":
			Cmd_AutoComplete(client)
		case "close":
			Cmd_Close(client)
		case "status":
			Cmd_Status(client)
		}
	}
	return 0
}

func main() {
	flag.Parse()

	var retval int
	if *server {
		retval = serverFunc()
	} else {
		retval = clientFunc()
	}
	os.Exit(retval)
}
