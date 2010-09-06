// This demo shows you upcoming gocode features, which will allow it to 
// understand semantics of each identifier in the code. This kind of thing
// leads to features like precise renaming of a language entity.
//
// Take a look.. I've written a vim plugin that highlights these language
// entities.
package main

type Dummy struct {
	a, b, c int
}

var a, b = struct{a int}{31337}, struct{b int}{666}
var z, y struct {
	a, b, c int
}

func main() {
	type Dummy *Dummy
	a.a = 10
	b.b = 20
	var A, B = struct{A int}{31337}, struct{B int}{666}
	C, D := struct{ABC int}{1}, struct{DEF int}{2}
	A.A = 10
	B.B = 20

	C.ABC = 30
	D.DEF = 40

	z.a = 1
	z.b = 2
	z.c = 3

	y.a = 1
	y.b = 2
	y.c = 3

	var T, Y, U int
	if T == 0 {
		Y, U := U, Y
		_, _ = Y, U
	}
}

func test() {
	var A struct {a, b, c int} = struct {a, b, c int}{1,2,3}
	A.a = 3
	A.b = 2
	A.c = 1
}
