package main

import (
	"flag"
	"fmt"
	"os"
	"syscall"
	"unsafe"
	"utf16"
)

func CreateSockFlag(name, desc string) *string {
	return flag.String(name, "tcp", desc)
}

func IsTerminationSignal(sig os.Signal) bool {
	return false
}

// Full path of the current executable
func GetExecutableFileName() string {
	kernel32, _ := syscall.LoadLibrary("kernel32.dll")
	defer syscall.FreeLibrary(kernel32)
	b := make([]uint16, syscall.MAX_PATH)
	getModuleFileName, _ := syscall.GetProcAddress(kernel32, "GetModuleFileNameW")
	ret, _, callErr := syscall.Syscall(uintptr(getModuleFileName),
		3, 0,
		uintptr(unsafe.Pointer(&b[0])),
		uintptr(len(b)))
	if callErr != 0 {
		panic(fmt.Sprintf("GetModuleFileNameW : err %d", int(callErr)))
	}
	return string(utf16.Decode(b[:ret]))
}
