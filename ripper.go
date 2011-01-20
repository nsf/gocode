package main

import (
	"go/token"
	"go/scanner"
)

// All the code in this file serves single purpose:
// It separates a function with the cursor inside and the rest of the code. I'm
// doing that, because sometimes parser is not able to recover itself from an
// error and the autocompletion results become less complete.

type TokPos struct {
	Tok token.Token
	Pos token.Pos
}

type TokCollection struct {
	tokens []TokPos
	fset   *token.FileSet
}

func (t *TokCollection) next(s *scanner.Scanner) bool {
	pos, tok, _ := s.Scan()
	if tok == token.EOF {
		return false
	}

	t.tokens = append(t.tokens, TokPos{tok, pos})
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
			lowpos = t.fset.Position(t.tokens[i].Pos).Offset
			lowi = i
		}
	}

	for i := lowi; i >= 0; i-- {
		if t.tokens[i].Tok == token.SEMICOLON {
			lowpos = t.fset.Position(t.tokens[i+1].Pos).Offset
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
			highpos = t.fset.Position(t.tokens[i].Pos).Offset
		}
	}

	return highpos
}

func (t *TokCollection) findOutermostScope(cursor int) (int, int) {
	pos := 0

	for i, tok := range t.tokens {
		if cursor <= t.fset.Position(tok.Pos).Offset {
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
	t.fset = token.NewFileSet()
	s := new(scanner.Scanner)
	s.Init(t.fset.AddFile("", t.fset.Base(), len(file)), file, nil, scanner.ScanComments|scanner.InsertSemis)
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
