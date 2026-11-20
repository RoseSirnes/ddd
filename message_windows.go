//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

func showError(msg string) {
	dll := syscall.NewLazyDLL("user32.dll")
	fn := dll.NewProc("MessageBoxW")
	body, _ := syscall.UTF16PtrFromString(msg)
	title, _ := syscall.UTF16PtrFromString("DDD")
	fn.Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x10)
}
