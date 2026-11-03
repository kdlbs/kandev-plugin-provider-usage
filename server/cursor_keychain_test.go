package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCursorSafeStorageKeychainAccountDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name            string
		current, legacy int32
		wantAccounts    []string
		wantExit        int
	}{
		{"current account", 0, 0, []string{"Cursor Key"}, 0},
		{"legacy account", cursorKeychainItemNotFound, 0, []string{"Cursor Key", "Cursor"}, 0},
		{"both absent", cursorKeychainItemNotFound, cursorKeychainItemNotFound, []string{"Cursor Key", "Cursor"}, 3},
		{"current denied", -25308, 0, []string{"Cursor Key"}, 2},
		{"legacy denied", cursorKeychainItemNotFound, -25293, []string{"Cursor Key", "Cursor"}, 2},
		{"current unavailable", -25291, 0, []string{"Cursor Key"}, 3},
		{"legacy unavailable", cursorKeychainItemNotFound, -25291, []string{"Cursor Key", "Cursor"}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var accounts []string
			password, exitCode := cursorSafeStorageKeychainPassword(func(service, account string) ([]byte, int32) {
				require.Equal(t, "Cursor Safe Storage", service)
				accounts = append(accounts, account)
				status := tc.current
				if account == "Cursor" {
					status = tc.legacy
				}
				// Data accompanying a failed query must never reach helper stdout.
				return []byte("synthetic-password"), status
			})
			require.Equal(t, tc.wantAccounts, accounts)
			require.Equal(t, tc.wantExit, exitCode)
			if exitCode == 0 {
				require.Equal(t, []byte("synthetic-password"), password)
			} else {
				require.Empty(t, password)
			}
		})
	}
}
