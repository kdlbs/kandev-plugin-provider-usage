package main

const cursorSafeStorageService = "Cursor Safe Storage"
const cursorKeychainItemNotFound int32 = -25300

// Prefer Electron's current account name, retaining the legacy account only
// when the current item is absent. A denial or locked Keychain must not trigger
// another credential read. The caller disables interaction before all lookups.
func cursorSafeStorageKeychainPassword(read func(service, account string) ([]byte, int32)) ([]byte, int) {
	for _, account := range []string{"Cursor Key", "Cursor"} {
		password, status := read(cursorSafeStorageService, account)
		switch status {
		case 0:
			return password, 0
		case cursorKeychainItemNotFound:
			continue
		case -25308, -25293, -25243, -128: // interaction required, auth failed, no access, canceled
			return nil, 2
		default:
			return nil, 3
		}
	}
	return nil, 3
}
