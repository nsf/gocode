package main

import (
	"io/ioutil"
	"strconv"
	"exec"
	"rpc"
	"flag"
	"time"
	"path"
	"fmt"
	"os"
)

var (
	server = flag.Bool("s", false, "run a server instead of a client")
	format = flag.String("f", "nice", "output format (vim | emacs | nice)")
	input = flag.String("in", "", "use this file instead of stdin input")
)

type Formatter interface {
	WriteEmpty()
	WriteCandidates(names, types, classes []string, num int)
}

//-------------------------------------------------------------------------
// NiceFormatter (just for testing, simple textual output)
//-------------------------------------------------------------------------

type NiceFormatter struct {
}

func (*NiceFormatter) WriteEmpty() {
	fmt.Printf("Nothing to complete.\n")
}

func (*NiceFormatter) WriteCandidates(names, types, classes []string, num int) {
	fmt.Printf("Found %d candidates:\n", len(names))
	for i := 0; i < len(names); i++ {
		abbr := fmt.Sprintf("%s %s %s", classes[i], names[i], types[i])
		if classes[i] == "func" {
			abbr = fmt.Sprintf("%s %s%s", classes[i], names[i], types[i][len("func "):])
		}
		fmt.Printf("  %s\n", abbr)
	}
}

//-------------------------------------------------------------------------
// VimFormatter
//-------------------------------------------------------------------------

type VimFormatter struct {
}

func (*VimFormatter) WriteEmpty() {
	fmt.Print("[]")
}

func (*VimFormatter) WriteCandidates(names, types, classes []string, num int) {
	fmt.Printf("[")
	for i := 0; i < len(names); i++ {
		word := names[i][num:]
		if classes[i] == "func" {
			word += "("
		}

		abbr := fmt.Sprintf("%s %s %s", classes[i], names[i], types[i])
		if classes[i] == "func" {
			abbr = fmt.Sprintf("%s %s%s", classes[i], names[i], types[i][len("func"):])
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

func (*EmacsFormatter) WriteCandidates(names, types, classes []string, num int) {
	for i := 0; i < len(names); i++ {
		name := names[i]
		hint := classes[i] + " " + types[i]
		if classes[i] == "func" {
			hint = types[i]
		}
		fmt.Printf("%s,,%s\n", name, hint)
	}
}

//-------------------------------------------------------------------------

func getFormatter() Formatter {
	switch *format {
	case "vim":
		return new(VimFormatter)
	case "emacs":
		return new(EmacsFormatter)
	case "nice":
		return new(NiceFormatter)
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

	filename := ""
	cursor := -1

	switch flag.NArg() {
	case 2:
		cursor, _ = strconv.Atoi(flag.Arg(1))
	case 3:
		filename = flag.Arg(1)
		cursor, _ = strconv.Atoi(flag.Arg(2))
	}

	if filename != "" && filename[0] != '/' {
		cwd, _ := os.Getwd()
		filename = path.Join(cwd, filename)
	}

	formatter := getFormatter()
	names, types, classes, partial := Client_AutoComplete(c, file, filename, cursor)
	if names == nil {
		formatter.WriteEmpty()
		return
	}

	if len(names) != len(types) || len(names) != len(classes) {
		panic("Lengths should match!")
	}

	formatter.WriteCandidates(names, types, classes, partial)
}

func Cmd_Close(c *rpc.Client) {
	Client_Close(c, 0)
}

func makeFDs() ([]*os.File, os.Error) {
	var fds [3]*os.File
	var err os.Error
	fds[0], err = os.Open("/dev/null", os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	fds[1], err = os.Open("/dev/null", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	fds[2], err = os.Open("/dev/null", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	// I know that technically it's possible here that there will be unclosed
	// file descriptors on exit. But since that kind of error will result in
	// a process shutdown anyway, I don't care much about that.

	return &fds, nil
}

func tryRunServer() os.Error {
	fds, err := makeFDs()
	if err != nil {
		return err
	}
	defer fds[0].Close()
	defer fds[1].Close()
	defer fds[2].Close()

	var path string
	path, err = exec.LookPath("gocode")
	if err != nil {
		return err
	}

	_, err = os.ForkExec(path, []string{"gocode", "-s"}, os.Environ(), "", fds)
	if err != nil {
		return err
	}
	return nil
}

func clientFunc() int {
	// client
	client, err := rpc.Dial("unix", getSocketFilename())
	if err != nil {
		err = tryRunServer()
		if err != nil {
			fmt.Printf("%s\n", err.String())
			return 1
		}
		time.Sleep(100000000) // 0.1
		client, err = rpc.Dial("unix", getSocketFilename())
		if err != nil {
			fmt.Printf("%s\n", err.String())
			return 1
		}
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
