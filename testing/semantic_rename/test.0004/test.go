package main

func test(x struct { a, b, c int }, y interface { Test() bool }) {
	if y.Test() {
		x.a = 10
		x.b = x.a
		x.c = x.a
	}
}

func main() {

}
