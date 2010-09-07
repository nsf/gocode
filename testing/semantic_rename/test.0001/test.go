// Anonymous structs and interfaces
package main

var a = struct{a int}{31337}
var b, c = struct{b int}{31337}, struct{c int}{31337}
var d struct {
	a, b, c int
}
var e, f struct {
	a, b, c int
}

var g interface {
	Test() bool
}
var h, i interface {
	Test() bool
}

var j = interface{Test() bool}(nil)
var k, l = interface{Test() bool}(nil),interface{Test() bool}(nil)

func main() {
	a.a = b.b
	c.c = a.a
	e.a = b.b
	f.a = b.b
	g.Test()
	h.Test()
	i.Test()
	j.Test()
	k.Test()
	l.Test()
}

var X map[string]struct{a,b,c int}
var Y, Z map[string]struct{d,e,f int}

func test() {
	a := struct{a int}{31337}
	b, c := struct{b int}{31337}, struct{c int}{31337}
	var d = struct{d int}{31337}
	var e, f = struct{e int}{31337}, struct{f int}{31337}
	var g struct {
		a, b, c int
	}
	var h, i struct {
		a, b, c int
	}
	a.a = b.b
	b.b = c.c
	d.d = e.e
	f.f = d.d
	g.a = h.a
	i.c = g.c

	j := interface{Test() bool}(nil)
	j.Test()

	var k, l = interface{Test() bool}(nil),interface{Test() bool}(nil)
	var m, n interface {
		Test() bool
	}
	k.Test()
	l.Test()
	m.Test()
	n.Test()

	A := X["0"].a
	D := Y["0"].d
	E := Z["0"].e

	i.c = A
	i.c = D
	i.c = E
}
