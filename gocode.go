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
	input = flag.String("in", "", "use this file instead of stdin input")
)

type Formatter interface {
	WriteEmpty()
	WriteCandidates(names, types, classes []string)
}

//-------------------------------------------------------------------------
// VimFormatter
//-------------------------------------------------------------------------

type VimFormatter struct {
}

func (*VimFormatter) WriteEmpty() {
	fmt.Print("[]")
}

func (*VimFormatter) WriteCandidates(names, types, classes []string) {
	fmt.Printf("[")
	for i := 0; i < len(names); i++ {
		// TODO: rip off part of the name somehow (?)
		word := names[i]
		if classes[i] == "func" {
			word += "("
		}

		abbr := fmt.Sprintf("%s %s %s", classes[i], names[i], types[i])
		if classes[i] == "func" {
			abbr = fmt.Sprintf("%s %s%s", classes[i], names[i], types[i][len("func "):])
		}
		fmt.Printf("{'word': '%s', 'abbr': '%s'}", word, abbr)
		if i != len(names)-1 {
			fmt.Printf(", ")
		}
	}
	fmt.Printf("]")
}

//-------------------------------------------------------------------------
// EmacsFormatter
//-------------------------------------------------------------------------

type EmacsFormatter struct {
}

func (*EmacsFormatter) WriteEmpty() {
}

func (*EmacsFormatter) WriteCandidates(names, types, classes []string) {
	for i := 0; i < len(names); i++ {
		name := names[i]
		hint := classes[i] + " " + types[i]
		if classes[i] == "func" {
			hint = types[i]
		}
		fmt.Printf("%s,,%s\n", name, hint)
	}
}

func getFormatter() Formatter {
	switch *format {
	case "vim":
		return new(VimFormatter)
	case "emacs":
		return new(EmacsFormatter)
	}
	return new(VimFormatter)
}

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
	var file []byte
	var err os.Error

	if *input != "" {
		file, err = ioutil.ReadFile(*input)
	} else {
		file, err = ioutil.ReadAll(os.Stdin)
	}

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
	formatter := getFormatter()
	names, types, classes := Client_AutoComplete(c, file, apropos, cursor)
	if names == nil {
		formatter.WriteEmpty()
		return
	}

	if len(names) != len(types) || len(names) != len(classes) {
		panic("Lengths should match!")
	}

	formatter.WriteCandidates(names, types, classes)
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
