//go:build windows

package restore

import (
	"syscall"
	"unsafe"
)

// setHiddenAttribute sets the hidden file attribute on Windows.
func setHiddenAttribute(path string) {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	attrs, err := syscall.GetFileAttributes(ptr)
	if err != nil {
		return
	}
	const fileAttributeHidden = 0x2
	attrs |= fileAttributeHidden
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setFileAttributes := kernel32.NewProc("SetFileAttributesW")
	setFileAttributes.Call(uintptr(unsafe.Pointer(ptr)), uintptr(attrs))
}
