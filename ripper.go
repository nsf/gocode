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

func (self *TokCollection) appendToken(pos token.Position, tok token.Token) {
	if self.tokens == nil {
		self.tokens = make([]TokPos, 0, 4)
	}

	n := len(self.tokens)
	if cap(self.tokens) < n+1 {
		s := make([]TokPos, n, n*2+1)
		copy(s, self.tokens)
		self.tokens = s
	}

	self.tokens = self.tokens[0:n+1]
	self.tokens[n] = TokPos{tok, pos}
}

func (self *TokCollection) next(s *scanner.Scanner) bool {
	pos, tok, _ := s.Scan()
	if tok == token.EOF {
		return false
	}

	self.appendToken(pos, tok)
	return true
}

func (self *TokCollection) findDeclBeg(pos int) int {
	lowest := 0
	lowpos := -1
	lowi := -1
	cur := 0
	for i := pos; i >= 0; i-- {
		switch self.tokens[i].Tok {
		case token.RBRACE:
			cur++
		case token.LBRACE:
			cur--
		}

		if cur < lowest {
			lowest = cur
			lowpos = self.tokens[i].Pos.Offset
			lowi = i
		}
	}

	for i := lowi; i >= 0; i-- {
		if self.tokens[i].Tok == token.SEMICOLON {
			lowpos = self.tokens[i+1].Pos.Offset
			break
		}
	}

	return lowpos
}

func (self *TokCollection) findDeclEnd(pos int) int {
	highest := 0
	highpos := -1
	cur := 0

	if self.tokens[pos].Tok == token.LBRACE {
		pos++
	}

	for i := pos; i < len(self.tokens); i++ {
		switch self.tokens[i].Tok {
		case token.RBRACE:
			cur++
		case token.LBRACE:
			cur--
		}

		if cur > highest {
			highest = cur
			highpos = self.tokens[i].Pos.Offset
		}
	}

	return highpos
}

func (self *TokCollection) findOutermostScope(cursor int) (int, int) {
	pos := 0

	for i, t := range self.tokens {
		if cursor <= t.Pos.Offset {
			break
		}
		pos = i
	}

	return self.findDeclBeg(pos), self.findDeclEnd(pos)
}

// return new cursor position, file without ripped part and the ripped part itself
// variants:
//   new-cursor, file-without-ripped-part, ripped-part
//   old-cursor, file, nil
func (self *TokCollection) ripOffDecl(file []byte, cursor int) (int, []byte, []byte) {
	s := new(scanner.Scanner)
	s.Init("", file, nil, scanner.ScanComments | scanner.InsertSemis)
	for self.next(s) {
	}

	beg, end := self.findOutermostScope(cursor)
	if beg == -1 || end == -1 {
		return cursor, file, nil
	}

	ripped := make([]byte, end + 1 - beg)
	copy(ripped, file[beg:end+1])

	newfile := make([]byte, len(file) - len(ripped))
	copy(newfile, file[0:beg])
	copy(newfile[beg:], file[end+1:])

	return cursor - beg, newfile, ripped
}

func RipOffDecl(file []byte, cursor int) (int, []byte, []byte) {
	tc := new(TokCollection)
	return tc.ripOffDecl(file, cursor)
}
