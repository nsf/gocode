package main

import (
	"fmt"
)

type highlights_formatter interface {
	write_ranges(ranges []highlight_range, num int)
}

type json_highlights_formatter struct{}

func (*json_highlights_formatter) write_ranges(ranges []highlight_range, num int) {
	if (ranges == nil) {
		fmt.Print("[]")
		return
	}

	fmt.Print("[")
	for i, c := range ranges {
		if i != 0 {
			fmt.Printf(", ")
		}
		fmt.Printf(`{"format": "%s", "line": "%d", "column": "%d", "length": "%d"}`,
			c.Format, c.Line, c.Column, c.Length)
	}
	fmt.Print("]\n")
}

func get_highlights_formatter(name string) highlights_formatter {
	return new(json_highlights_formatter)
}
