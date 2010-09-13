package main

type X struct {
	a, b, c int
}

func main() {
	x := &X{
		a: 1,
		b: 2,
		c: 3,
	}

	a := x.a
	b := x.b
	c := x.c
	x.a = x.b
	_, _, _ = a, b, c
}
