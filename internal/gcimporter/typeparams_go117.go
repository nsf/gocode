//go:build !go1.18
// +build !go1.18

package gcimporter

import (
	"go/types"
)

func unsupported() {
	panic("type parameters are unsupported at this go version")
}

// TypeParam is a placeholder type, as type parameters are not supported at
// this Go version. Its methods panic on use.
type TypeParam struct{ types.Type }

func (*TypeParam) Index() int             { unsupported(); return 0 }
func (*TypeParam) Constraint() types.Type { unsupported(); return nil }
func (*TypeParam) Obj() *types.TypeName   { unsupported(); return nil }

type TypeParamList struct{}

func (*TypeParamList) Len() int          { return 0 }
func (*TypeParamList) At(int) *TypeParam { unsupported(); return nil }

func typeParamsForNamed(named *types.Named) *TypeParamList {
	return nil
}

func typeParamsForRecv(sig *types.Signature) *TypeParamList {
	return nil
}

func typeParamsForSig(sig *types.Signature) *TypeParamList {
	return nil
}

func typeParamsToTuple(tparams *TypeParamList) *types.Tuple {
	return types.NewTuple()
}

func originType(t types.Type) types.Type {
	return t
}

// Term holds information about a structural type restriction.
type Term struct {
	tilde bool
	typ   types.Type
}

func (m *Term) Tilde() bool      { return m.tilde }
func (m *Term) Type() types.Type { return m.typ }
func (m *Term) String() string {
	pre := ""
	if m.tilde {
		pre = "~"
	}
	return pre + m.typ.String()
}

// NewTerm creates a new placeholder term type.
func NewTerm(tilde bool, typ types.Type) *Term {
	return &Term{tilde, typ}
}

// Union is a placeholder type, as type parameters are not supported at this Go
// version. Its methods panic on use.
type Union struct{ types.Type }

func (*Union) String() string         { unsupported(); return "" }
func (*Union) Underlying() types.Type { unsupported(); return nil }
func (*Union) Len() int               { return 0 }
func (*Union) Term(i int) *Term       { unsupported(); return nil }

var comparableType = comparable{}

type comparable struct{}

func (t comparable) Underlying() types.Type { return t }
func (t comparable) String() string         { return "comparable" }
