package main

import (
	"io/ioutil"
	"bufio"
	"exec"
	"json"
	"fmt"
	"os"
)

const BLUEBG = "\033[44m"
const NC = "\033[0m"

type DeclDesc struct {
	Offset int
	Length int
}

func getSMap(smap_raw []byte) []DeclDesc {
	smap := new([]DeclDesc)
	err := json.Unmarshal(smap_raw, smap)
	if err != nil {
		panic(err.String())
	}
	return *smap
}

func retrieveSMap(filename string) []byte {
	path, err := exec.LookPath("gocode")
	if err != nil {
		panic(err.String())
	}

	cwd, err := os.Getwd()
	if err != nil {
		panic(err.String())
	}
	p, err := exec.Run(path, []string{"gocode", "smap", filename}, os.Environ(), cwd, exec.Pipe, exec.Pipe, exec.Pipe)
	if err != nil {
		panic(err.String())
	}
	defer p.Close()

	data, err := ioutil.ReadAll(p.Stdout)
	if err != nil {
		panic(err.String())
	}

	return data
}

func inSMap(offset int, smap []DeclDesc) int {
	for i := range smap {
		o, l := smap[i].Offset, smap[i].Length
		if offset >= o && offset < o + l {
			return l
		}
	}
	return 0
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: showsmap <filename>")
		return
	}

	filename := os.Args[1]

	smap := getSMap(retrieveSMap(filename))
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err.String())
	}

	out := bufio.NewWriter(os.Stdout)

	i := 0
	for i < len(data) {
		l := inSMap(i, smap)
		if l > 0 {
			fmt.Fprintf(out, BLUEBG)
			out.Write(data[i:i+l])
			fmt.Fprintf(out, NC)
			i += l
		} else {
			out.WriteByte(data[i])
			i++
		}
	}
	out.Flush()
}
