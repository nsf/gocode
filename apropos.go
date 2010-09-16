package main

import (
	"utf8"
	"unicode"
	"go/parser"
)

type DeclApropos struct {
	Decl    *Decl
	Partial string
}

func utf8MoveBackwards(file []byte, cursor int) int {
	for {
		cursor--
		if cursor <= 0 {
			return 0
		}
		if utf8.RuneStart(file[cursor]) {
			return cursor
		}
	}
	return 0
}

func isIdent(rune int) bool {
	return unicode.IsDigit(rune) || unicode.IsLetter(rune) || rune == '_'
}

func skipIdent(file []byte, cursor int) int {
	for {
		letter, _ := utf8.DecodeRune(file[cursor:])
		if !isIdent(letter) {
			return cursor
		}
		cursor = utf8MoveBackwards(file, cursor)
		if cursor <= 0 {
			return 0
		}
	}
	return 0
}

var pairs = map[byte]byte{
	')': '(',
	']': '[',
}

func skipToPair(file []byte, cursor int) int {
	right := file[cursor]
	left := pairs[file[cursor]]
	balance := 1
	for balance != 0 {
		cursor--
		if cursor <= 0 {
			return 0
		}
		switch file[cursor] {
		case right:
			balance++
		case left:
			balance--
		}
	}
	return cursor
}

func findExpr(file []byte) []byte {
	const (
		LAST_NONE = iota
		LAST_DOT
		LAST_PAREN
		LAST_IDENT
	)
	last := LAST_NONE
	cursor := len(file)
	cursor = utf8MoveBackwards(file, cursor)
loop:
	for {
		c := file[cursor]
		letter, _ := utf8.DecodeRune(file[cursor:])
		switch c {
		case '.':
			cursor = utf8MoveBackwards(file, cursor)
			last = LAST_DOT
		case ')', ']':
			if last == LAST_IDENT {
				break loop
			}
			cursor = utf8MoveBackwards(file, skipToPair(file, cursor))
			last = LAST_PAREN
		default:
			if isIdent(letter) {
				cursor = skipIdent(file, cursor)
				last = LAST_IDENT
			} else {
				break loop
			}
		}
	}
	return file[cursor+1:]
}

func (c *AutoCompleteContext) deduceExpr(file []byte, partial string) *DeclApropos {
	e := findExpr(file)
	expr, err := parser.ParseExpr("", e)
	if err != nil {
		return nil
	}
	typedecl := exprToDecl(expr, c.current.scope)
	if typedecl != nil {
		return &DeclApropos{typedecl, partial}
	}
	return nil
}

func (c *AutoCompleteContext) deduceDecl(file []byte, cursor int) *DeclApropos {
	orig := cursor

	if cursor < 0 {
		return nil
	}
	if cursor == 0 {
		return &DeclApropos{nil, ""}
	}

	// figure out what is just before the cursor
	cursor = utf8MoveBackwards(file, cursor)
	if file[cursor] == '.' {
		// we're '<whatever>.'
		// figure out decl, Parital is ""
		return c.deduceExpr(file[:cursor], "")
	} else {
		letter, _ := utf8.DecodeRune(file[cursor:])
		if isIdent(letter) {
			// we're '<whatever>.<ident>'
			// parse <ident> as Partial and figure out decl
			cursor = skipIdent(file, cursor)
			partial := string(file[cursor+1 : orig])
			if file[cursor] == '.' {
				return c.deduceExpr(file[:cursor], partial)
			} else {
				return &DeclApropos{nil, partial}
			}
		}
	}

	return &DeclApropos{nil, ""}
}
