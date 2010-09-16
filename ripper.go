package main

import (
	"go/token"
	"go/scanner"
)

type TokPos struct {
	Tok token.Token
	Pos token.Position
}

type TokCollection struct {
	tokens []TokPos
}

func (t *TokCollection) appendToken(pos token.Position, tok token.Token) {
	if t.tokens == nil {
		t.tokens = make([]TokPos, 0, 4)
	}

	n := len(t.tokens)
	if cap(t.tokens) < n+1 {
		s := make([]TokPos, n, n*2+1)
		copy(s, t.tokens)
		t.tokens = s
	}

	t.tokens = t.tokens[:n+1]
	t.tokens[n] = TokPos{tok, pos}
}

func (t *TokCollection) next(s *scanner.Scanner) bool {
	pos, tok, _ := s.Scan()
	if tok == token.EOF {
		return false
	}

	t.appendToken(pos, tok)
	return true
}

func (t *TokCollection) findDeclBeg(pos int) int {
	lowest := 0
	lowpos := -1
	lowi := -1
	cur := 0
	for i := pos; i >= 0; i-- {
		switch t.tokens[i].Tok {
		case token.RBRACE:
			cur++
		case token.LBRACE:
			cur--
		}

		if cur < lowest {
			lowest = cur
			lowpos = t.tokens[i].Pos.Offset
			lowi = i
		}
	}

	for i := lowi; i >= 0; i-- {
		if t.tokens[i].Tok == token.SEMICOLON {
			lowpos = t.tokens[i+1].Pos.Offset
			break
		}
	}

	return lowpos
}

func (t *TokCollection) findDeclEnd(pos int) int {
	highest := 0
	highpos := -1
	cur := 0

	if t.tokens[pos].Tok == token.LBRACE {
		pos++
	}

	for i := pos; i < len(t.tokens); i++ {
		switch t.tokens[i].Tok {
		case token.RBRACE:
			cur++
		case token.LBRACE:
			cur--
		}

		if cur > highest {
			highest = cur
			highpos = t.tokens[i].Pos.Offset
		}
	}

	return highpos
}

func (t *TokCollection) findOutermostScope(cursor int) (int, int) {
	pos := 0

	for i, tok := range t.tokens {
		if cursor <= tok.Pos.Offset {
			break
		}
		pos = i
	}

	return t.findDeclBeg(pos), t.findDeclEnd(pos)
}

// return new cursor position, file without ripped part and the ripped part itself
// variants:
//   new-cursor, file-without-ripped-part, ripped-part
//   old-cursor, file, nil
func (t *TokCollection) ripOffDecl(file []byte, cursor int) (int, []byte, []byte) {
	s := new(scanner.Scanner)
	s.Init("", file, nil, scanner.ScanComments|scanner.InsertSemis)
	for t.next(s) {
	}

	beg, end := t.findOutermostScope(cursor)
	if beg == -1 || end == -1 {
		return cursor, file, nil
	}

	ripped := make([]byte, end+1-beg)
	copy(ripped, file[beg:end+1])

	newfile := make([]byte, len(file)-len(ripped))
	copy(newfile, file[:beg])
	copy(newfile[beg:], file[end+1:])

	return cursor - beg, newfile, ripped
}

func RipOffDecl(file []byte, cursor int) (int, []byte, []byte) {
	tc := new(TokCollection)
	return tc.ripOffDecl(file, cursor)
}
