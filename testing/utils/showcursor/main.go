package main

import (
	"io/ioutil"
	"strconv"
	"fmt"
	"os"
)

const RED = "\033[1;31m"
const NC = "\033[0m"

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Usage: showcursor <filename> <cursor>")
		return
	}

	filename := os.Args[1]
	cursor, err := strconv.Atoi(os.Args[2])
	if err != nil {
		panic(err.String())
	}

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err.String())
	}

	os.Stdout.Write(data[0:cursor])
	if data[cursor] == '\n' {
		// If cursor points to '\n' (usual case for autocompletion),
		// print dummy symbol '#'.
		fmt.Fprintf(os.Stdout, "%s#%s", RED, NC)
		os.Stdout.Write(data[cursor:])
	} else {
		// If cursor is not a '\n' symbol, print that symbol with red color.
		fmt.Fprintf(os.Stdout,
			    "%s%s%s",
			    RED,
			    string(data[cursor:cursor+1]),
			    NC)
		os.Stdout.Write(data[cursor+1:])
	}
}
