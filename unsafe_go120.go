//go:build go1.20
// +build go1.20

package main

var g_builtin_unsafe_package = []byte(`
import
$$
package unsafe
	func @"".Alignof(x ArbitraryType) uintptr
	func @"".Offsetof(x ArbitraryType) uintptr
	func @"".Sizeof(x ArbitraryType) uintptr
	type @"".Pointer *ArbitraryType
	func @"".Slice(ptr *ArbitraryType, len IntegerType) []ArbitraryType
	func @"".SliceData(slice []ArbitraryType) *ArbitraryType
	func @"".Add(ptr Pointer, len IntegerType) Pointer
	func @"".String(ptr *byte, len IntegerType) string
	func @"".StringData(str string) *byte
$$
`)
