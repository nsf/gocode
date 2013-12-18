package main

import (
	"go/ast"
	"go/token"
	"bytes"
)

type highlight_visitor struct {
	tmpbuf     *bytes.Buffer
	ranges    []highlight_range
	ctx        *highlight_context
	parsedAst *ast.File
}

func new_highlight_visitor(ctx *highlight_context, parsedAst *ast.File) *highlight_visitor {
	v := new(highlight_visitor)
	v.parsedAst = parsedAst
	v.tmpbuf = bytes.NewBuffer(make([]byte, 0, 1024))
	v.ranges = make([]highlight_range, 0, 64)
	v.ctx = ctx
	return v
}

func (v *highlight_visitor) IsInFile(pos token.Pos) bool {
	var position token.Position
	position = v.ctx.current.fset.Position(pos)
	return len(position.Filename) == 0 || position.Filename == v.ctx.current.name
}

func (v *highlight_visitor) AddRange(format string, start token.Pos, end token.Pos) {
	var startPos token.Position
	var endPos token.Position
	startPos = v.ctx.current.fset.Position(start)
	endPos = v.ctx.current.fset.Position(end)
	if len(startPos.Filename) == 0 || startPos.Filename == v.ctx.current.name {
		var r highlight_range
		r.Format = format
		r.Line = int32(startPos.Line)
		r.Column = int32(startPos.Column)
		r.Length = int32(endPos.Offset - startPos.Offset)
		v.ranges = append(v.ranges, r)
	}
}

func (v *highlight_visitor) AddIdent(format string, id *ast.Ident) {
	var startPos token.Position
	startPos = v.ctx.current.fset.Position(id.Pos())
	if len(startPos.Filename) == 0 || startPos.Filename == v.ctx.current.name {
		var r highlight_range
		r.Format = format
		r.Line = int32(startPos.Line)
		r.Column = int32(startPos.Column)
		r.Length = int32(len(id.Name))
		v.ranges = append(v.ranges, r)
	}
}

// Visits AST and produces ranges to highlight
// Most lexical elements are ignored - they are out of scope for semantic highligher
// Result only consist of erroneous ranges and identifiers (without keywords).
// Produced ranges list
//	error: bad expression, decl or stmt
//  package: package name
//  field: struct field name or anything accessed by dot (we mean expression at right side)
//  func: function name
//  label: jump label
//  type: declarated type name
//  var: variable name
func (v *highlight_visitor) Visit(node ast.Node) (w ast.Visitor) {
	if node == nil {
		return nil
	}

	switch t := node.(type) {
	case *ast.BadDecl:
		v.AddRange("error", t.From, t.To)
		break
	case *ast.BadExpr:
		v.AddRange("error", t.From, t.To)
		break
	case *ast.BadStmt:
		v.AddRange("error", t.From, t.To)
		break
	case *ast.BasicLit:
		return nil
	case *ast.BranchStmt:
		return nil
	case *ast.CommentGroup:
		return nil
	case *ast.Comment:
		return nil
	case *ast.Ellipsis:
		return nil
	case *ast.EmptyStmt:
		return nil
	case *ast.Ident: {
		if t.Obj != nil {
			var format string
			switch {
				case t.Obj.Kind == ast.Bad:
					format = "error"
					break
				case t.Obj.Kind == ast.Pkg:
					format = "package"
					break
				case t.Obj.Kind == ast.Con:
					format = "const"
					break
				case t.Obj.Kind == ast.Typ:
					format = "type"
					break
				case t.Obj.Kind == ast.Var:
					format = "var"
					break
				case t.Obj.Kind == ast.Fun:
					format = "func"
					break
				case t.Obj.Kind == ast.Lbl:
					format = "label"
					break
			}
			if len(format) != 0 {
				v.AddIdent(format, t)
			}
		}
	}
	case *ast.File:
		return v
	case *ast.SelectorExpr:
		v.AddIdent("field", t.Sel)
		break
	default:
		break
	}

	if v.IsInFile(node.Pos()) {
		return v
	}
	return nil
}
