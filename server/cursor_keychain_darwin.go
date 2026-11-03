package main

import (
	"os"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Runs only in the bounded helper subprocess. Disable interaction BEFORE any
// credential lookup, including legacy ACL checks. No `security -w` invocation:
// that command can prompt during every background poll after a denial.
func cursorSafeStorageHelperMain() int {
	lib, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return 3
	}
	defer purego.Dlclose(lib)
	var noUI func(uint8) int32
	var find func(uintptr, uint32, string, uint32, string, *uint32, *unsafe.Pointer, uintptr) int32
	var free func(uintptr, unsafe.Pointer) int32
	for _, symbol := range []struct {
		name   string
		target any
	}{
		{"SecKeychainSetUserInteractionAllowed", &noUI},
		{"SecKeychainFindGenericPassword", &find},
		{"SecKeychainItemFreeContent", &free},
	} {
		address, err := purego.Dlsym(lib, symbol.name)
		if err != nil {
			return 3
		}
		purego.RegisterFunc(symbol.target, address)
	}
	if noUI(0) != 0 {
		return 3
	}
	const service, account = "Cursor Safe Storage", "Cursor"
	var length uint32
	var data unsafe.Pointer
	status := find(0, uint32(len(service)), service, uint32(len(account)), account, &length, &data, 0)
	if status != 0 {
		switch status {
		case -25308, -25293, -25243, -128: // interaction required, auth failed, no access, canceled
			return 2
		default:
			return 3
		}
	}
	defer free(0, data)
	if data == nil || length == 0 || length > 4096 {
		return 3
	}
	if _, err := os.Stdout.Write(unsafe.Slice((*byte)(data), int(length))); err != nil {
		return 3
	}
	return 0
}
