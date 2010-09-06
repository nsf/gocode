package main

import (
	"io/ioutil"
	"sort"
	"exec"
	"json"
	"fmt"
	"os"
)

type RenameDeclDesc struct {
	Offset int
	Line int
	Col int
}

type RenameDesc struct {
	Length int
	Filename string
	Decls []RenameDeclDesc
}

// Sort interface
func (d *RenameDesc) Less(i, j int) bool { return d.Decls[i].Offset < d.Decls[j].Offset }
func (d *RenameDesc) Len() int { return len(d.Decls) }
func (d *RenameDesc) Swap(i, j int) { d.Decls[i], d.Decls[j] = d.Decls[j], d.Decls[i] }

func getRename(raw []byte) []RenameDesc {
	rename := new([]RenameDesc)
	err := json.Unmarshal(raw, rename)
	if err != nil {
		panic(err.String())
	}
	return *rename
}

func retrieveRename(filename string, cursor string) []byte {
	path, err := exec.LookPath("gocode")
	if err != nil {
		panic(err.String())
	}

	cwd, err := os.Getwd()
	if err != nil {
		panic(err.String())
	}
	p, err := exec.Run(path, []string{"gocode", "rename", filename, cursor},
			   os.Environ(), cwd, exec.Pipe, exec.Pipe, exec.Pipe)
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

func main() {
	if len(os.Args) != 4 {
		fmt.Println("Usage: rename <filename> <cursor> <new name>")
		return
	}

	filename := os.Args[1]
	cursor := os.Args[2]
	newname := os.Args[3]

	rename := getRename(retrieveRename(filename, cursor))
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err.String())
	}

	if len(rename) != 0 {
		sort.Sort(&rename[0])

		cur := 0
		for _, desc := range rename[0].Decls {
			os.Stdout.Write(data[cur:desc.Offset])
			os.Stdout.Write([]byte(newname))
			cur = desc.Offset + rename[0].Length
		}

		os.Stdout.Write(data[cur:])
	} else {
		os.Stdout.Write(data)
	}
}
