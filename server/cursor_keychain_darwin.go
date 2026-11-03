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
	password, exitCode := cursorSafeStorageKeychainPassword(func(service, account string) ([]byte, int32) {
		var length uint32
		var data unsafe.Pointer
		status := find(0, uint32(len(service)), service, uint32(len(account)), account, &length, &data, 0)
		if status != 0 {
			return nil, status
		}
		defer free(0, data)
		if data == nil || length == 0 || length > 4096 {
			return nil, -25291 // unavailable
		}
		return append([]byte(nil), unsafe.Slice((*byte)(data), int(length))...), 0
	})
	if exitCode != 0 {
		return exitCode
	}
	if _, err := os.Stdout.Write(password); err != nil {
		return 3
	}
	return 0
}
