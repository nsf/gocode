//go:build go1.18
// +build go1.18

package gcimporter

import (
	"go/types"
)

type TypeParam = types.TypeParam
type TypeParamList = types.TypeParamList

func typeParamsForNamed(named *types.Named) *types.TypeParamList {
	return named.TypeParams()
}

func typeParamsForRecv(sig *types.Signature) *types.TypeParamList {
	return sig.RecvTypeParams()
}

func typeParamsForSig(sig *types.Signature) *types.TypeParamList {
	return sig.TypeParams()
}

func typeParamsToTuple(tparams *types.TypeParamList) *types.Tuple {
	if tparams == nil {
		return types.NewTuple()
	}
	n := tparams.Len()
	ar := make([]*types.Var, n)
	for i := 0; i < n; i++ {
		tp := tparams.At(i)
		obj := tp.Obj()
		ar[i] = types.NewVar(obj.Pos(), obj.Pkg(), obj.Name(), tp.Constraint())
	}
	return types.NewTuple(ar...)
}

func originType(t types.Type) types.Type {
	if named, ok := t.(*types.Named); ok {
		t = named.Origin()
	}
	return t
}

type Union = types.Union
type Term = types.Term
