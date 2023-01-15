//go:build !go1.17
// +build !go1.17

package main

var g_builtin_unsafe_package = []byte(`
import
$$
package unsafe
	func @"".Alignof(x ArbitraryType) uintptr
	func @"".Offsetof(x ArbitraryType) uintptr
	func @"".Sizeof(x ArbitraryType) uintptr
	type @"".Pointer *ArbitraryType
$$
`)
