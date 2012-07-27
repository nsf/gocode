package main

import (
	"go/parser"
	"unicode"
	"unicode/utf8"
)

type cursor_context struct {
	decl    *decl
	partial string
}

type bytes_iterator struct {
	data   []byte
	cursor int
}

// return the character under the cursor
func (this *bytes_iterator) char() byte {
	return this.data[this.cursor]
}

// return the rune under the cursor
func (this *bytes_iterator) rune() rune {
	r, _ := utf8.DecodeRune(this.data[this.cursor:])
	return r
}

// move cursor backwards to the next valid utf8 rune start, or 0
func (this *bytes_iterator) move_backwards() {
	for this.cursor != 0 {
		this.cursor--
		if utf8.RuneStart(this.char()) {
			return
		}
	}
}

var g_unicode_ident_set = []*unicode.RangeTable{
	unicode.Letter,
	unicode.Digit,
	{R16: []unicode.Range16{{'_', '_', 1}}},
}

// move cursor backwards, stop at the first rune that is not from
// 'g_unicode_ident_set', or 0
func (this *bytes_iterator) skip_ident() {
	for this.cursor != 0 {
		r := this.rune()

		// stop if 'r' is not [a-zA-Z0-9_] (unicode correct though)
		if !unicode.IsOneOf(g_unicode_ident_set, r) {
			return
		}
		this.move_backwards()
	}
}

var g_bracket_pairs = map[byte]byte{
	')': '(',
	']': '[',
}

// when the cursor is at the ')' or ']', move the cursor to an opposite bracket
// pair, this functions takes inner bracker pairs into account
func (this *bytes_iterator) skip_to_bracket_pair() {
	right := this.char()
	left := g_bracket_pairs[right]
	balance := 1
	for balance != 0 {
		this.cursor--
		if this.cursor == 0 {
			return
		}
		switch this.char() {
		case right:
			balance++
		case left:
			balance--
		}
	}
}

// starting from the end of the 'file', move backwards and return a slice of a
// valid Go expression
func (this *bytes_iterator) extract_go_expr() []byte {
	const (
		last_none = iota
		last_dot
		last_paren
		last_ident
	)
	last := last_none
	orig := this.cursor
	this.move_backwards()
loop:
	for {
		if this.cursor == 0 {
			return this.data[:orig]
		}
		r := this.rune()
		switch r {
		case '.':
			this.move_backwards()
			last = last_dot
		case ')', ']':
			if last == last_ident {
				break loop
			}
			this.skip_to_bracket_pair()
			this.move_backwards()
			last = last_paren
		default:
			if unicode.IsOneOf(g_unicode_ident_set, r) {
				this.skip_ident()
				last = last_ident
			} else {
				break loop
			}
		}
	}
	return this.data[this.cursor+1 : orig]
}

// this function is called when the cursor is at the '.' and you need to get the
// declaration before that dot
func (c *auto_complete_context) deduce_cursor_decl(iter *bytes_iterator) *decl {
	e := string(iter.extract_go_expr())
	expr, err := parser.ParseExpr(e)
	if err != nil {
		return nil
	}
	return expr_to_decl(expr, c.current.scope)
}

// deduce cursor context, it includes the declaration under the cursor and partial identifier
// (usually a part of the name of the child declaration)
func (c *auto_complete_context) deduce_cursor_context(file []byte, cursor int) (cursor_context, bool) {
	if cursor <= 0 {
		return cursor_context{nil, ""}, true
	}

	orig := cursor
	iter := bytes_iterator{file, cursor}

	// figure out what is just before the cursor
	iter.move_backwards()
	if iter.char() == '.' {
		// we're '<whatever>.'
		// figure out decl, Parital is ""
		decl := c.deduce_cursor_decl(&iter)
		return cursor_context{decl, ""}, decl != nil
	}

	r := iter.rune()
	if unicode.IsOneOf(g_unicode_ident_set, r) {
		// we're '<whatever>.<ident>'
		// parse <ident> as Partial and figure out decl
		iter.skip_ident()
		partial := string(iter.data[iter.cursor+1 : orig])
		if iter.char() == '.' {
			decl := c.deduce_cursor_decl(&iter)
			return cursor_context{decl, partial}, decl != nil
		} else {
			return cursor_context{nil, partial}, true
		}
	}

	return cursor_context{nil, ""}, true
}
