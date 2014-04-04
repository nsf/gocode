package main

import (
	"fmt"
	"strings"
)

//-------------------------------------------------------------------------
// formatter interfaces
//-------------------------------------------------------------------------

type formatter interface {
	writeCandidates(candidates []candidate, num int)
}

//-------------------------------------------------------------------------
// niceFormatter (just for testing, simple textual output)
//-------------------------------------------------------------------------

type niceFormatter struct{}

func (*niceFormatter) writeCandidates(candidates []candidate, num int) {
	if candidates == nil {
		fmt.Printf("Nothing to complete.\n")
		return
	}

	fmt.Printf("Found %d candidates:\n", len(candidates))
	for _, c := range candidates {
		abbr := fmt.Sprintf("%s %s %s", c.Class, c.Name, c.Type)
		if c.Class == declFunc {
			abbr = fmt.Sprintf("%s %s%s", c.Class, c.Name, c.Type[len("func"):])
		}
		fmt.Printf("  %s\n", abbr)
	}
}

//-------------------------------------------------------------------------
// vimFormatter
//-------------------------------------------------------------------------

type vimFormatter struct{}

func (*vimFormatter) writeCandidates(candidates []candidate, num int) {
	if candidates == nil {
		fmt.Print("[0, []]")
		return
	}

	fmt.Printf("[%d, [", num)
	for i, c := range candidates {
		if i != 0 {
			fmt.Printf(", ")
		}

		word := c.Name
		if c.Class == declFunc {
			word += "("
			if strings.HasPrefix(c.Type, "func()") {
				word += ")"
			}
		}

		abbr := fmt.Sprintf("%s %s %s", c.Class, c.Name, c.Type)
		if c.Class == declFunc {
			abbr = fmt.Sprintf("%s %s%s", c.Class, c.Name, c.Type[len("func"):])
		}
		fmt.Printf("{'word': '%s', 'abbr': '%s', 'info': '%s'}", word, abbr, abbr)
	}
	fmt.Printf("]]")
}

//-------------------------------------------------------------------------
// goditFormatter
//-------------------------------------------------------------------------

type goditFormatter struct{}

func (*goditFormatter) writeCandidates(candidates []candidate, num int) {
	fmt.Printf("%d,,%d\n", num, len(candidates))
	for _, c := range candidates {
		contents := c.Name
		if c.Class == declFunc {
			contents += "("
			if strings.HasPrefix(c.Type, "func()") {
				contents += ")"
			}
		}

		display := fmt.Sprintf("%s %s %s", c.Class, c.Name, c.Type)
		if c.Class == declFunc {
			display = fmt.Sprintf("%s %s%s", c.Class, c.Name, c.Type[len("func"):])
		}
		fmt.Printf("%s,,%s\n", display, contents)
	}
}

//-------------------------------------------------------------------------
// emacsFormatter
//-------------------------------------------------------------------------

type emacsFormatter struct{}

func (*emacsFormatter) writeCandidates(candidates []candidate, num int) {
	for _, c := range candidates {
		hint := c.Class.String() + " " + c.Type
		if c.Class == declFunc {
			hint = c.Type
		}
		fmt.Printf("%s,,%s\n", c.Name, hint)
	}
}

//-------------------------------------------------------------------------
// csvFormatter
//-------------------------------------------------------------------------

type csvFormatter struct{}

func (*csvFormatter) writeCandidates(candidates []candidate, num int) {
	for _, c := range candidates {
		fmt.Printf("%s,,%s,,%s\n", c.Class, c.Name, c.Type)
	}
}

//-------------------------------------------------------------------------
// jsonFormatter
//-------------------------------------------------------------------------

type jsonFormatter struct{}

func (*jsonFormatter) writeCandidates(candidates []candidate, num int) {
	if candidates == nil {
		fmt.Print("[]")
		return
	}

	fmt.Printf(`[%d, [`, num)
	for i, c := range candidates {
		if i != 0 {
			fmt.Printf(", ")
		}
		fmt.Printf(`{"class": "%s", "name": "%s", "type": "%s"}`,
			c.Class, c.Name, c.Type)
	}
	fmt.Print("]]")
}

//-------------------------------------------------------------------------

func getFormatter(name string) formatter {
	switch name {
	case "vim":
		return new(vimFormatter)
	case "emacs":
		return new(emacsFormatter)
	case "nice":
		return new(niceFormatter)
	case "csv":
		return new(csvFormatter)
	case "json":
		return new(jsonFormatter)
	case "godit":
		return new(goditFormatter)
	}
	return new(niceFormatter)
}
