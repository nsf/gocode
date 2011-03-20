package main

import (
	"io/ioutil"
	"strconv"
	"bytes"
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
	format = flag.String("f", "nice", "output format (vim | emacs | nice | csv | json)")
	input  = flag.String("in", "", "use this file instead of stdin input")
	sock   = CreateSockFlag("sock", "socket type (unix | tcp)")
	addr   = flag.String("addr", "localhost:37373", "address for tcp socket")
)

// TODO: find a better place for this function
// returns truncated 'data' and amount of bytes skipped (for cursor pos adjustment)
func filterOutShebang(data []byte) ([]byte, int) {
	if len(data) > 2 && data[0] == '#' && data[1] == '!' {
		newline := bytes.Index(data, []byte("\n"))
		if newline != -1 && len(data) > newline+1 {
			return data[newline+1:], newline+1
		}
	}
	return data, 0
}

//-------------------------------------------------------------------------
// Formatter interfaces
//-------------------------------------------------------------------------

type FormatterEmpty interface {
	WriteEmpty()
}

type FormatterCandidates interface {
	WriteCandidates(names, types, classes []string, num int)
}

//-------------------------------------------------------------------------
// NiceFormatter (just for testing, simple textual output)
//-------------------------------------------------------------------------

type NiceFormatter struct{}

func (*NiceFormatter) WriteEmpty() {
	fmt.Printf("Nothing to complete.\n")
}

func (*NiceFormatter) WriteCandidates(names, types, classes []string, num int) {
	fmt.Printf("Found %d candidates:\n", len(names))
	for i := 0; i < len(names); i++ {
		abbr := fmt.Sprintf("%s %s %s", classes[i], names[i], types[i])
		if classes[i] == "func" {
			abbr = fmt.Sprintf("%s %s%s", classes[i], names[i], types[i][len("func"):])
		}
		fmt.Printf("  %s\n", abbr)
	}
}

//-------------------------------------------------------------------------
// VimFormatter
//-------------------------------------------------------------------------

type VimFormatter struct{}

func (*VimFormatter) WriteEmpty() {
	fmt.Print("[0, []]")
}

func (*VimFormatter) WriteCandidates(names, types, classes []string, num int) {
	fmt.Printf("[%d, [", num)
	for i := 0; i < len(names); i++ {
		word := names[i]
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
	fmt.Printf("]]")
}

//-------------------------------------------------------------------------
// EmacsFormatter
//-------------------------------------------------------------------------

type EmacsFormatter struct{}

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
// CSVFormatter
//-------------------------------------------------------------------------

type CSVFormatter struct{}

func (*CSVFormatter) WriteCandidates(names, types, classes []string, num int) {
	for i := 0; i < len(names); i++ {
		fmt.Printf("%s,,%s,,%s\n", classes[i], names[i], types[i])
	}
}

//-------------------------------------------------------------------------
// JSONFormatter
//-------------------------------------------------------------------------

type JSONFormatter struct{}

func (*JSONFormatter) WriteEmpty() {
	fmt.Print("[]")
}

func (*JSONFormatter) WriteCandidates(names, types, classes []string, num int) {
	fmt.Printf(`[%d, [`, num)
	for i := 0; i < len(names); i++ {
		fmt.Printf(`{"class": "%s", "name": "%s", "type": "%s"}`,
			classes[i], names[i], types[i])
		if i != len(names)-1 {
			fmt.Printf(", ")
		}
	}
	fmt.Print("]]")
}

//-------------------------------------------------------------------------

func getFormatter() interface{} {
	switch *format {
	case "vim":
		return new(VimFormatter)
	case "emacs":
		return new(EmacsFormatter)
	case "nice":
		return new(NiceFormatter)
	case "csv":
		return new(CSVFormatter)
	case "json":
		return new(JSONFormatter)
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
	readConfig(&Config)

	addr := *addr
	if *sock == "unix" {
		addr = getSocketFilename()
		if fileExists(addr) {
			fmt.Printf("unix socket: '%s' already exists\n", addr)
			return 1
		}
	}
	daemon = NewDaemon(*sock, addr)
	if *sock == "unix" {
		// cleanup unix socket file
		defer os.Remove(addr)
	}

	rpcremote := new(RPCRemote)
	rpc.Register(rpcremote)

	daemon.acr.Loop()
	return 0
}

func cmdStatus(c *rpc.Client) {
	fmt.Printf("%s\n", Client_Status(c, 0))
}

func cmdAutoComplete(c *rpc.Client) {
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

	var skipped int
	file, skipped = filterOutShebang(file)

	filename := ""
	cursor := -1

	switch flag.NArg() {
	case 2:
		cursor, _ = strconv.Atoi(flag.Arg(1))
	case 3:
		filename = flag.Arg(1)
		cursor, _ = strconv.Atoi(flag.Arg(2))
	}

	cursor -= skipped

	if filename != "" && !IsAbsPath(filename) {
		cwd, _ := os.Getwd()
		filename = path.Join(cwd, filename)
	}

	formatter := getFormatter()
	names, types, classes, partial := Client_AutoComplete(c, file, filename, cursor)
	if names == nil {
		if f, ok := formatter.(FormatterEmpty); ok {
			f.WriteEmpty()
		}
		return
	}

	if f, ok := formatter.(FormatterCandidates); ok {
		f.WriteCandidates(names, types, classes, partial)
	}
}

func cmdClose(c *rpc.Client) {
	Client_Close(c, 0)
}

func cmdDropCache(c *rpc.Client) {
	Client_DropCache(c, 0)
}

func cmdSet(c *rpc.Client) {
	switch flag.NArg() {
	case 1:
		fmt.Print(Client_Set(c, "", ""))
	case 2:
		fmt.Print(Client_Set(c, flag.Arg(1), ""))
	case 3:
		fmt.Print(Client_Set(c, flag.Arg(1), flag.Arg(2)))
	}
}

func tryRunServer() os.Error {
	path, err := exec.LookPath("gocode")
	if err != nil {
		return err
	}

	args := []string{"gocode", "-s", "-sock", *sock, "-addr", *addr}
	procattr := os.ProcAttr{"", os.Environ(), []*os.File{nil, nil, nil}}
	_, err = os.StartProcess(path, args, &procattr)

	if err != nil {
		return err
	}
	return nil
}

func tryToConnect(network, address string) (client *rpc.Client, err os.Error) {
	t := 0
	for {
		client, err = rpc.Dial(network, address)
		if err != nil && t < 1000 {
			time.Sleep(10e6) // wait 10 milliseconds
			t += 10
			continue
		}
		break
	}

	return
}

func clientFunc() int {
	addr := *addr
	if *sock == "unix" {
		addr = getSocketFilename()
	}

	// client
	client, err := rpc.Dial(*sock, addr)
	if err != nil {
		err = tryRunServer()
		if err != nil {
			fmt.Printf("%s\n", err.String())
			return 1
		}
		client, err = tryToConnect(*sock, addr)
		if err != nil {
			fmt.Printf("%s\n", err.String())
			return 1
		}
	}
	defer client.Close()

	if flag.NArg() > 0 {
		switch flag.Arg(0) {
		case "autocomplete":
			cmdAutoComplete(client)
		case "close":
			cmdClose(client)
		case "status":
			cmdStatus(client)
		case "drop-cache":
			cmdDropCache(client)
		case "set":
			cmdSet(client)
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
