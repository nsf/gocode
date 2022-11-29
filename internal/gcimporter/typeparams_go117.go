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
