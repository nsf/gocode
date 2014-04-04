package main

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
)

type cursorContext struct {
	decl    *decl
	partial string
}

type tokenIterator struct {
	tokens      []tokenItem
	tokenIndex int
}

type tokenItem struct {
	off int
	tok token.Token
	lit string
}

func (i tokenItem) Literal() string {
	if i.tok.IsLiteral() {
		return i.lit
	} else {
		return i.tok.String()
	}
}

func (this *tokenIterator) token() tokenItem {
	return this.tokens[this.tokenIndex]
}

func (this *tokenIterator) previousToken() bool {
	if this.tokenIndex <= 0 {
		return false
	}
	this.tokenIndex--
	return true
}

var gBracketPairs = map[token.Token]token.Token{
	token.RPAREN: token.LPAREN,
	token.RBRACK: token.LBRACK,
}

// when the cursor is at the ')' or ']', move the cursor to an opposite bracket
// pair, this functions takes inner bracker pairs into account
func (this *tokenIterator) skipToBracketPair() bool {
	right := this.token().tok
	left := gBracketPairs[right]
	return this.skipToLeftBracket(left, right)
}

func (this *tokenIterator) skipToLeftBracket(left, right token.Token) bool {
	// TODO: Make this functin recursive.
	if this.token().tok == left {
		return true
	}
	balance := 1
	for balance != 0 {
		this.previousToken()
		if this.tokenIndex == 0 {
			return false
		}
		switch this.token().tok {
		case right:
			balance++
		case left:
			balance--
		}
	}
	return true
}

// Move the cursor to the open brace of the current block, taking inner blocks
// into account.
func (this *tokenIterator) skipToOpenBrace() bool {
	return this.skipToLeftBracket(token.LBRACE, token.RBRACE)
}

// tryExtractStructInitExpr tries to match the current cursor position as being inside a struct
// initialization expression of the form:
// &X{
// 	Xa: 1,
// 	Xb: 2,
// }
// Nested struct initialization expressions are handled correctly.
func (this *tokenIterator) tryExtractStructInitExpr() []byte {
	for this.tokenIndex >= 0 {
		if !this.skipToOpenBrace() {
			return nil
		}

		if !this.previousToken() {
			return nil
		}

		return []byte(this.token().Literal())
	}
	return nil
}

// starting from the end of the 'file', move backwards and return a slice of a
// valid Go expression
func (this *tokenIterator) extractGoExpr() []byte {
	// TODO: Make this function recursive.
	last := token.ILLEGAL
	orig := this.tokenIndex
loop:
	for {
		if this.tokenIndex == 0 {
			return makeExpr(this.tokens[:orig])
		}
		switch r := this.token().tok; r {
		case token.PERIOD:
			this.previousToken()
			last = r
		case token.RPAREN, token.RBRACK:
			if last == token.IDENT {
				// ")ident" or "]ident".
				break loop
			}
			this.skipToBracketPair()
			this.previousToken()
			last = r
		case token.SEMICOLON, token.LPAREN, token.LBRACE, token.COLON, token.COMMA:
			// If we reach one of these tokens, the expression is definitely over.
			break loop
		default:
			this.previousToken()
			last = r
		}
	}
	return makeExpr(this.tokens[this.tokenIndex+1 : orig])
}

// Given a slice of tokenItem, reassembles them into the original literal expression.
func makeExpr(tokens []tokenItem) []byte {
	e := ""
	for _, t := range tokens {
		e += t.Literal()
	}
	return []byte(e)
}

// this function is called when the cursor is at the '.' and you need to get the
// declaration before that dot
func (c *autoCompleteContext) deduceCursorDecl(iter *tokenIterator) *decl {
	e := string(iter.extractGoExpr())
	expr, err := parser.ParseExpr(e)
	if err != nil {
		return nil
	}
	return exprToDecl(expr, c.current.scope)
}

func newTokenIterator(src []byte, cursor int) tokenIterator {
	tokens := make([]tokenItem, 0, 1000)
	var s scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	s.Init(file, src, nil, 0)
	tokenIndex := 0
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		off := fset.Position(pos).Offset
		tokens = append(tokens, tokenItem{
			off: off,
			tok: tok,
			lit: lit,
		})
		if cursor > off {
			tokenIndex++
		}
	}
	return tokenIterator{
		tokens:      tokens,
		tokenIndex: tokenIndex,
	}
}

// deduce cursor context, it includes the declaration under the cursor and partial identifier
// (usually a part of the name of the child declaration)
func (c *autoCompleteContext) deduceCursorContext(file []byte, cursor int) (cursorContext, bool) {
	if cursor <= 0 {
		return cursorContext{nil, ""}, true
	}

	iter := newTokenIterator(file, cursor)

	// figure out what is just before the cursor
	iter.previousToken()
	switch r := iter.token().tok; r {
	case token.PERIOD:
		// we're '<whatever>.'
		// figure out decl, Partial is ""
		decl := c.deduceCursorDecl(&iter)
		return cursorContext{decl, ""}, decl != nil
	case token.IDENT, token.VAR:
		// we're '<whatever>.<ident>'
		// parse <ident> as Partial and figure out decl
		partial := iter.token().Literal()
		iter.previousToken()
		if iter.token().tok == token.PERIOD {
			decl := c.deduceCursorDecl(&iter)
			return cursorContext{decl, partial}, decl != nil
		} else {
			return cursorContext{nil, partial}, true
		}
	case token.COMMA, token.LBRACE:
		// Try to parse the current expression as a structure initialization.
		data := iter.tryExtractStructInitExpr()
		if data == nil {
			return cursorContext{nil, ""}, true
		}

		expr, err := parser.ParseExpr(string(data))
		if err != nil {
			return cursorContext{nil, ""}, true
		}
		decl := exprToDecl(expr, c.current.scope)
		if decl == nil {
			return cursorContext{nil, ""}, true
		}

		// Make sure whatever is before the opening brace is a struct.
		switch decl.typ.(type) {
		case *ast.StructType:
			// TODO: Return partial.
			return cursorContext{structMembersOnly(decl), ""}, true
		}
	}

	return cursorContext{nil, ""}, true
}

// structMembersOnly returns a copy of decl with all its children of type function stripped out.
// This is used when returning matches for struct initialization expressions, for which it does not
// make sense to suggest a function name associated with the struct.
func structMembersOnly(decl *decl) *decl {
	newDecl := *decl
	for k, d := range newDecl.children {
		switch d.typ.(type) {
		case *ast.FuncType:
			// Strip functions from the list.
			delete(newDecl.children, k)
		}
	}
	return &newDecl
}

// deduce the type of the expression under the cursor, a bit of copy & paste from the method
// above, returns true if deduction was successful (even if the result of it is nil)
func (c *autoCompleteContext) deduceCursorTypePkg(file []byte, cursor int) (ast.Expr, string, bool) {
	if cursor <= 0 {
		return nil, "", true
	}

	iter := newTokenIterator(file, cursor)

	// read backwards to extract expression
	e := string(iter.extractGoExpr())

	expr, err := parser.ParseExpr(e)
	if err != nil {
		return nil, "", false
	} else {
		t, scope, _ := inferType(expr, c.current.scope, -1)
		return t, lookupPkg(getTypePath(t), scope), t != nil
	}
}
