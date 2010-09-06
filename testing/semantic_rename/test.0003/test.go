package main

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
