package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/rpc"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func doClient() int {
	addr := *gAddr
	if *gSock == "unix" {
		addr = getSocketFilename()
	}

	// client
	client, err := rpc.Dial(*gSock, addr)
	if err != nil {
		if *gSock == "unix" && fileExists(addr) {
			os.Remove(addr)
		}

		err = tryRunServer()
		if err != nil {
			fmt.Printf("%s\n", err.Error())
			return 1
		}
		client, err = tryToConnect(*gSock, addr)
		if err != nil {
			fmt.Printf("%s\n", err.Error())
			return 1
		}
	}
	defer client.Close()

	if flag.NArg() > 0 {
		switch flag.Arg(0) {
		case "autocomplete":
			cmdAutoComplete(client)
		case "cursortype":
			cmdCursorTypePkg(client)
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

func tryRunServer() error {
	path := getExecutableFilename()
	args := []string{os.Args[0], "-s", "-sock", *gSock, "-addr", *gAddr}
	cwd, _ := os.Getwd()
	procattr := os.ProcAttr{Dir: cwd, Env: os.Environ(), Files: []*os.File{nil, nil, nil}}
	p, err := os.StartProcess(path, args, &procattr)

	if err != nil {
		return err
	}
	return p.Release()
}

func tryToConnect(network, address string) (client *rpc.Client, err error) {
	t := 0
	for {
		client, err = rpc.Dial(network, address)
		if err != nil && t < 1000 {
			time.Sleep(10 * time.Millisecond)
			t += 10
			continue
		}
		break
	}

	return
}

func prepareFileFilenameCursor() ([]byte, string, int) {
	var file []byte
	var err error

	if *gInput != "" {
		file, err = ioutil.ReadFile(*gInput)
	} else {
		file, err = ioutil.ReadAll(os.Stdin)
	}

	if err != nil {
		panic(err.Error())
	}

	var skipped int
	file, skipped = filterOutShebang(file)

	filename := *gInput
	cursor := -1

	offset := ""
	switch flag.NArg() {
	case 2:
		offset = flag.Arg(1)
	case 3:
		filename = flag.Arg(1) // Override default filename
		offset = flag.Arg(2)
	}

	if offset != "" {
		if offset[0] == 'c' || offset[0] == 'C' {
			cursor, _ = strconv.Atoi(offset[1:])
			cursor = charToByteOffset(file, cursor)
		} else {
			cursor, _ = strconv.Atoi(offset)
		}
	}

	cursor -= skipped
	if filename != "" && !filepath.IsAbs(filename) {
		cwd, _ := os.Getwd()
		filename = filepath.Join(cwd, filename)
	}
	return file, filename, cursor
}

//-------------------------------------------------------------------------
// commands
//-------------------------------------------------------------------------

func cmdStatus(c *rpc.Client) {
	fmt.Printf("%s\n", clientStatus(c, 0))
}

func cmdAutoComplete(c *rpc.Client) {
	var env gocodeEnv
	env.get()
	file, filename, cursor := prepareFileFilenameCursor()
	f := getFormatter(*gFormat)
	f.writeCandidates(clientAutoComplete(c, file, filename, cursor, env))
}

func cmdCursorTypePkg(c *rpc.Client) {
	file, filename, cursor := prepareFileFilenameCursor()
	typ, pkg := clientCursorTypePkg(c, file, filename, cursor)
	fmt.Printf("%s,,%s\n", typ, pkg)
}

func cmdClose(c *rpc.Client) {
	clientClose(c, 0)
}

func cmdDropCache(c *rpc.Client) {
	clientDropCache(c, 0)
}

func cmdSet(c *rpc.Client) {
	switch flag.NArg() {
	case 1:
		fmt.Print(clientSet(c, "\x00", "\x00"))
	case 2:
		fmt.Print(clientSet(c, flag.Arg(1), "\x00"))
	case 3:
		fmt.Print(clientSet(c, flag.Arg(1), flag.Arg(2)))
	}
}
