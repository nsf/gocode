package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
)

var (
	procShGetFolderPath   = shell32.NewProc("SHGetFolderPathW")
	procGetModuleFileName = kernel32.NewProc("GetModuleFileNameW")
)

func createSockFlag(name, desc string) *string {
	return flag.String(name, "tcp", desc)
}

// Full path of the current executable
func getExecutableFilename() string {
	b := make([]uint16, syscall.MAX_PATH)
	ret, _, err := syscall.Syscall(procGetModuleFileName.Addr(), 3,
		0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	if int(ret) == 0 {
		panic(fmt.Sprintf("GetModuleFileNameW : err %d", int(err)))
	}
	return syscall.UTF16ToString(b)
}

const (
	csidlAppdata = 0x1a
)

func getAppdataFolderPath() string {
	b := make([]uint16, syscall.MAX_PATH)
	ret, _, err := syscall.Syscall6(procShGetFolderPath.Addr(), 5,
		0, csidlAppdata, 0, 0, uintptr(unsafe.Pointer(&b[0])), 0)
	if int(ret) != 0 {
		panic(fmt.Sprintf("SHGetFolderPathW : err %d", int(err)))
	}
	return syscall.UTF16ToString(b)
}

func configDir() string {
	return filepath.Join(getAppdataFolderPath(), "gocode")
}

func configFile() string {
	return filepath.Join(getAppdataFolderPath(), "gocode", "config.json")
}
