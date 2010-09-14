package main

type X struct {
	a, b, c int
}

type Mega map[string]string

const STR_1 = "1"
const STR_2 = "2"
const STR_3 = "3"

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

	y := Mega{
		STR_1: "111",
		STR_2: "222",
		STR_3: "333",
		"sega": "mega",
	}
	d := y[STR_1]
	e := y[STR_2]
	f := y[STR_3]
	_, _, _ = d, e, f
}
