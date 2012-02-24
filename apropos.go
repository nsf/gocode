package main

import (
	"go/parser"
	"unicode"
	"unicode/utf8"
)

type decl_apropos struct {
	decl    *decl
	partial string
}

func utf8_move_backwards(file []byte, cursor int) int {
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

func is_ident(r rune) bool {
	return unicode.IsDigit(r) || unicode.IsLetter(r) || r == '_'
}

func skip_ident(file []byte, cursor int) int {
	for {
		letter, _ := utf8.DecodeRune(file[cursor:])
		if !is_ident(letter) {
			return cursor
		}
		cursor = utf8_move_backwards(file, cursor)
		if cursor <= 0 {
			return 0
		}
	}
	return 0
}

var g_bracket_pairs = map[byte]byte{
	')': '(',
	']': '[',
}

func skip_to_pair(file []byte, cursor int) int {
	right := file[cursor]
	left := g_bracket_pairs[file[cursor]]
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

func find_expr(file []byte) []byte {
	const (
		LAST_NONE = iota
		LAST_DOT
		LAST_PAREN
		LAST_IDENT
	)
	last := LAST_NONE
	cursor := len(file)
	cursor = utf8_move_backwards(file, cursor)
loop:
	for {
		c := file[cursor]
		letter, _ := utf8.DecodeRune(file[cursor:])
		switch c {
		case '.':
			cursor = utf8_move_backwards(file, cursor)
			last = LAST_DOT
		case ')', ']':
			if last == LAST_IDENT {
				break loop
			}
			cursor = utf8_move_backwards(file, skip_to_pair(file, cursor))
			last = LAST_PAREN
		default:
			if is_ident(letter) {
				cursor = skip_ident(file, cursor)
				last = LAST_IDENT
			} else {
				break loop
			}
		}
	}
	return file[cursor+1:]
}

func (c *auto_complete_context) deduce_expr(file []byte, partial string) *decl_apropos {
	e := string(find_expr(file))
	expr, err := parser.ParseExpr(e)
	if err != nil {
		return nil
	}
	typedecl := expr_to_decl(expr, c.current.scope)
	if typedecl != nil {
		return &decl_apropos{typedecl, partial}
	}
	return nil
}

func (c *auto_complete_context) deduce_decl(file []byte, cursor int) *decl_apropos {
	orig := cursor

	if cursor < 0 {
		return nil
	}
	if cursor == 0 {
		return &decl_apropos{nil, ""}
	}

	// figure out what is just before the cursor
	cursor = utf8_move_backwards(file, cursor)
	if file[cursor] == '.' {
		// we're '<whatever>.'
		// figure out decl, Parital is ""
		return c.deduce_expr(file[:cursor], "")
	} else {
		letter, _ := utf8.DecodeRune(file[cursor:])
		if is_ident(letter) {
			// we're '<whatever>.<ident>'
			// parse <ident> as Partial and figure out decl
			cursor = skip_ident(file, cursor)
			partial := string(file[cursor+1 : orig])
			if file[cursor] == '.' {
				return c.deduce_expr(file[:cursor], partial)
			} else {
				return &decl_apropos{nil, partial}
			}
		}
	}

	return &decl_apropos{nil, ""}
}
