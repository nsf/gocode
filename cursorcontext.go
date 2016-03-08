package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"log"
)

type cursor_context struct {
	decl         *decl
	partial      string
	struct_field bool
	decl_import  bool
}

type token_iterator struct {
	tokens      []token_item
	token_index int
}

type token_item struct {
	off int
	tok token.Token
	lit string
}

func (i token_item) literal() string {
	if i.tok.IsLiteral() {
		return i.lit
	} else {
		return i.tok.String()
	}
	return ""
}

func new_token_iterator(src []byte, cursor int) token_iterator {
	tokens := make([]token_item, 0, 1000)
	var s scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	s.Init(file, src, nil, 0)
	for {
		pos, tok, lit := s.Scan()
		off := fset.Position(pos).Offset
		if tok == token.EOF || cursor <= off {
			break
		}
		tokens = append(tokens, token_item{
			off: off,
			tok: tok,
			lit: lit,
		})
	}
	return token_iterator{
		tokens:      tokens,
		token_index: len(tokens) - 1,
	}
}

func (this *token_iterator) token() token_item {
	return this.tokens[this.token_index]
}

func (this *token_iterator) go_back() bool {
	if this.token_index <= 0 {
		return false
	}
	this.token_index--
	return true
}

var bracket_pairs_map = map[token.Token]token.Token{
	token.RPAREN: token.LPAREN,
	token.RBRACK: token.LBRACK,
	token.RBRACE: token.LBRACE,
}

func (ti *token_iterator) skip_to_left(left, right token.Token) bool {
	if ti.token().tok == left {
		return true
	}
	balance := 1
	for balance != 0 {
		if !ti.go_back() {
			return false
		}
		switch ti.token().tok {
		case right:
			balance++
		case left:
			balance--
		}
	}
	return true
}

// when the cursor is at the ')' or ']' or '}', move the cursor to an opposite
// bracket pair, this functions takes nested bracket pairs into account
func (this *token_iterator) skip_to_balanced_pair() bool {
	right := this.token().tok
	left := bracket_pairs_map[right]
	return this.skip_to_left(left, right)
}

// Move the cursor to the open brace of the current block, taking nested blocks
// into account.
func (this *token_iterator) skip_to_left_curly() bool {
	return this.skip_to_left(token.LBRACE, token.RBRACE)
}

// Extract the type expression right before the enclosing curly bracket block.
// Examples (# - the cursor):
//   &lib.Struct{Whatever: 1, Hel#} // returns "lib.Struct"
//   X{#}                           // returns X
// The idea is that we check if this type expression is a type and it is, we
// can apply special filtering for autocompletion results.
// Sadly, this doesn't cover anonymous structs.
func (ti *token_iterator) extract_struct_type() string {
	if !ti.skip_to_left_curly() {
		return ""
	}
	if !ti.go_back() {
		return ""
	}
	if ti.token().tok != token.IDENT {
		return ""
	}
	b := ti.token().literal()
	if !ti.go_back() {
		return b
	}
	if ti.token().tok != token.PERIOD {
		return b
	}
	if !ti.go_back() {
		return b
	}
	if ti.token().tok != token.IDENT {
		return b
	}
	return ti.token().literal() + "." + b
}

// Starting from the token under the cursor move back and extract something
// that resembles a valid Go primary expression. Examples of primary expressions
// from Go spec:
//   x
//   2
//   (s + ".txt")
//   f(3.1415, true)
//   Point{1, 2}
//   m["foo"]
//   s[i : j + 1]
//   obj.color
//   f.p[i].x()
//
// As you can see we can move through all of them using balanced bracket
// matching and applying simple rules
// E.g.
//   Point{1, 2}.m["foo"].s[i : j + 1].MethodCall(a, func(a, b int) int { return a + b }).
// Can be seen as:
//   Point{    }.m[     ].s[         ].MethodCall(                                      ).
// Which boils the rules down to these connected via dots:
//   ident
//   ident[]
//   ident{}
//   ident()
// Of course there are also slightly more complicated rules for brackets:
//   ident{}.ident()[5][4](), etc.
func (this *token_iterator) extract_go_expr() string {
	orig := this.token_index

	// Contains the type of the previously scanned token (initialized with
	// the token right under the cursor). This is the token to the *right* of
	// the current one.
	prev := this.token().tok
loop:
	for {
		if !this.go_back() {
			return token_items_to_string(this.tokens[:orig])
		}
		switch this.token().tok {
		case token.PERIOD:
			// If the '.' is not followed by IDENT, it's invalid.
			if prev != token.IDENT {
				break loop
			}
		case token.IDENT:
			// Valid tokens after IDENT are '.', '[', '{' and '('.
			switch prev {
			case token.PERIOD, token.LBRACK, token.LBRACE, token.LPAREN:
				// all ok
			default:
				break loop
			}
		case token.RBRACE:
			// This one can only be a part of type initialization, like:
			//   Dummy{}.Hello()
			// It is valid Go if Hello method is defined on a non-pointer receiver.
			if prev != token.PERIOD {
				break loop
			}
			this.skip_to_balanced_pair()
		case token.RPAREN, token.RBRACK:
			// After ']' and ')' their opening counterparts are valid '[', '(',
			// as well as the dot.
			switch prev {
			case token.PERIOD, token.LBRACK, token.LPAREN:
				// all ok
			default:
				break loop
			}
			this.skip_to_balanced_pair()
		default:
			break loop
		}
		prev = this.token().tok
	}
	expr := token_items_to_string(this.tokens[this.token_index+1 : orig])
	if *g_debug {
		log.Printf("extracted expression tokens: %s", expr)
	}
	return expr
}

// Given a slice of token_item, reassembles them into the original literal
// expression.
func token_items_to_string(tokens []token_item) string {
	var buf bytes.Buffer
	for _, t := range tokens {
		buf.WriteString(t.literal())
	}
	return buf.String()
}

// this function is called when the cursor is at the '.' and you need to get the
// declaration before that dot
func (c *auto_complete_context) deduce_cursor_decl(iter *token_iterator) *decl {
	expr, err := parser.ParseExpr(iter.extract_go_expr())
	if err != nil {
		return nil
	}
	return expr_to_decl(expr, c.current.scope)
}

// try to find and extract the surrounding struct literal type
func (c *auto_complete_context) deduce_struct_type_decl(iter *token_iterator) *decl {
	typ := iter.extract_struct_type()
	if typ == "" {
		return nil
	}

	expr, err := parser.ParseExpr(typ)
	if err != nil {
		return nil
	}
	decl := type_to_decl(expr, c.current.scope)
	if decl == nil {
		return nil
	}
	if _, ok := decl.typ.(*ast.StructType); !ok {
		return nil
	}
	return decl
}

// Entry point from autocompletion, the function looks at text before the cursor
// and figures out the declaration the cursor is on. This declaration is
// used in filtering the resulting set of autocompletion suggestions.
func (c *auto_complete_context) deduce_cursor_context(file []byte, cursor int) (cursor_context, bool) {
	if cursor <= 0 {
		return cursor_context{}, true
	}

	iter := new_token_iterator(file, cursor)
	if len(iter.tokens) == 0 {
		return cursor_context{}, false
	}

	// figure out what is just before the cursor
	switch tok := iter.token(); tok.tok {
	case token.STRING:
		// make sure cursor is inside the string
		s := tok.literal()
		if len(s) > 1 && s[len(s)-1] == '"' && tok.off+len(s) <= cursor {
			return cursor_context{}, true
		}
		// now figure out if inside an import declaration
		var ptok = token.STRING
		for iter.go_back() {
			itok := iter.token().tok
			switch itok {
			case token.STRING:
				switch ptok {
				case token.SEMICOLON, token.IDENT, token.PERIOD:
				default:
					return cursor_context{}, true
				}
			case token.LPAREN, token.SEMICOLON:
				switch ptok {
				case token.STRING, token.IDENT, token.PERIOD:
				default:
					return cursor_context{}, true
				}
			case token.IDENT, token.PERIOD:
				switch ptok {
				case token.STRING:
				default:
					return cursor_context{}, true
				}
			case token.IMPORT:
				switch ptok {
				case token.STRING, token.IDENT, token.PERIOD, token.LPAREN:
					path_len := cursor - tok.off
					path := s[1:path_len]
					return cursor_context{decl_import: true, partial: path}, true
				default:
					return cursor_context{}, true
				}
			default:
				return cursor_context{}, true
			}
			ptok = itok
		}
	case token.PERIOD:
		// we're '<whatever>.'
		// figure out decl, Partial is ""
		decl := c.deduce_cursor_decl(&iter)
		return cursor_context{decl: decl}, decl != nil
	case token.IDENT, token.TYPE, token.CONST, token.VAR, token.FUNC, token.PACKAGE:
		// we're '<whatever>.<ident>'
		// parse <ident> as Partial and figure out decl
		var partial string
		if tok.tok == token.IDENT {
			// Calculate the offset of the cursor position within the identifier.
			// For instance, if we are 'ab#c', we want partial_len = 2 and partial = ab.
			partial_len := cursor - tok.off

			// If it happens that the cursor is past the end of the literal,
			// means there is a space between the literal and the cursor, think
			// of it as no context, because that's what it really is.
			if partial_len > len(tok.literal()) {
				return cursor_context{}, true
			}
			partial = tok.literal()[0:partial_len]
		} else {
			// Do not try to truncate if it is not an identifier.
			partial = tok.literal()
		}

		iter.go_back()
		switch iter.token().tok {
		case token.PERIOD:
			decl := c.deduce_cursor_decl(&iter)
			return cursor_context{decl: decl, partial: partial}, decl != nil
		case token.COMMA, token.LBRACE:
			// This can happen for struct fields:
			// &Struct{Hello: 1, Wor#} // (# - the cursor)
			// Let's try to find the struct type
			decl := c.deduce_struct_type_decl(&iter)
			return cursor_context{
				decl:         decl,
				partial:      partial,
				struct_field: decl != nil,
			}, true
		default:
			return cursor_context{partial: partial}, true
		}
	case token.COMMA, token.LBRACE:
		// Try to parse the current expression as a structure initialization.
		decl := c.deduce_struct_type_decl(&iter)
		return cursor_context{
			decl:         decl,
			partial:      "",
			struct_field: decl != nil,
		}, true
	}

	return cursor_context{}, true
}
