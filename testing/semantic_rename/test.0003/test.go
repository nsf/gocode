// just a random test of misc stuff
package main

import "strings"
import "io/ioutil"
import "hash/crc32"
import "fmt"
import "os"

type file struct {
	contents []byte
	name string
}

func loadFile(filename string) *file {
	var err os.Error
	// here, I'm trying to be nasty and using the same name for var and type
	f := new(file)
	f.name = filename
	f.contents, err = ioutil.ReadFile(filename)
	if err != nil {
		panic(err.String())
	}

	return f
}

func (file *file) printHash() {
	h := crc32.NewIEEE()
	h.Write(file.contents)
	fmt.Printf("%s: %X\n", file.name, h.Sum32())
}

type mystring string

func (mystring mystring) printHash() {
	h := crc32.NewIEEE()
	h.Write([]byte(string(mystring)))
	fmt.Printf("%X\n", h.Sum32())
}

func main() {
	// again, using name 'file' as much as possible
	// you can't deal with that using regexps based search & replace
	f := loadFile("file.txt")
	f.printHash() // printHash 1

	if f.isAbsolute() {
		fmt.Println("yay!")
	}
	fmt.Println(f)

	// note: the same method name, but on a different type, gocode 'rename' 
	// can easily handle that
	mystring("just a simple test").printHash() // printHash 2
}

func (file *file) isAbsolute() bool {
	return strings.HasPrefix(file.name, "/")
}

// Stringer interface
func (file *file) String() string {
	return "<file: " + file.name + ">"
}
