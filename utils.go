package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"
)

// returns truncated 'data' and amount of bytes skipped (for cursor pos adjustment)
func filterOutShebang(data []byte) ([]byte, int) {
	if len(data) > 2 && data[0] == '#' && data[1] == '!' {
		newline := bytes.Index(data, []byte("\n"))
		if newline != -1 && len(data) > newline+1 {
			return data[newline+1:], newline + 1
		}
	}
	return data, 0
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return true
}

func charToByteOffset(s []byte, offsetC int) (offsetB int) {
	for offsetB = 0; offsetC > 0 && offsetB < len(s); offsetB++ {
		if utf8.RuneStart(s[offsetB]) {
			offsetC--
		}
	}
	return offsetB
}

func xdgHomeDir() string {
	xdghome := os.Getenv("XDG_CONFIG_HOME")
	if xdghome == "" {
		xdghome = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return xdghome
}

func hasPrefix(s, prefix string, ignorecase bool) bool {
	if ignorecase {
		s = strings.ToLower(s)
		prefix = strings.ToLower(prefix)
	}
	return strings.HasPrefix(s, prefix)
}

//-------------------------------------------------------------------------
// printBacktrace
//
// a nicer backtrace printer than the default one
//-------------------------------------------------------------------------

var gBacktraceMutex sync.Mutex

func printBacktrace(err interface{}) {
	gBacktraceMutex.Lock()
	defer gBacktraceMutex.Unlock()
	fmt.Printf("panic: %v\n", err)
	i := 2
	for {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		f := runtime.FuncForPC(pc)
		fmt.Printf("%d(%s): %s:%d\n", i-1, f.Name(), file, line)
		i++
	}
	fmt.Println("")
}

//-------------------------------------------------------------------------
// File reader goroutine
//
// It's a bad idea to block multiple goroutines on file I/O. Creates many
// threads which fight for HDD. Therefore only single goroutine should read HDD
// at the same time.
// -------------------------------------------------------------------------

type fileReadRequest struct {
	filename string
	out      chan fileReadResponse
}

type fileReadResponse struct {
	data  []byte
	error error
}

type fileReaderType struct {
	in chan fileReadRequest
}

func newFileReader() *fileReaderType {
	this := new(fileReaderType)
	this.in = make(chan fileReadRequest)
	go func() {
		var rsp fileReadResponse
		for {
			req := <-this.in
			rsp.data, rsp.error = ioutil.ReadFile(req.filename)
			req.out <- rsp
		}
	}()
	return this
}

func (this *fileReaderType) readFile(filename string) ([]byte, error) {
	req := fileReadRequest{
		filename,
		make(chan fileReadResponse),
	}
	this.in <- req
	rsp := <-req.out
	return rsp.data, rsp.error
}

var fileReader = newFileReader()
