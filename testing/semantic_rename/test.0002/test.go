// scope testing
package main

type file struct {
	file string
}

func (file *file) String() string {
	return file.file
}

func GetName(file *file) string {
	return file.file
}

type Test struct {
	file *file
}

var X int

func test() {
	go func(){
		var x int
		x = X
		_ = x
	}()

	X := "die"
	_ = X
}

func main() {
	files := [...]file{file{"1.txt"}, file{"2.txt"}}

	var test *Test
	var file *file
	test.file = file
	if file == nil {
		file := files
		for i, file := range file {
			file.String()
			_ = i
		}
	} else {
		var array interface{}
		switch file := array.(type) {
		case int:
			panic("Oh no!")
		}
	}
}
