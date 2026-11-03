package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Independent OpenSSL AES-128-CBC vector with Python hashlib PBKDF2-SHA1:
// password synthetic-password, salt saltysalt, iterations 1003, IV 16 spaces.
// Plaintext is cursorAuthFixture(); no real credentials are used.
const cursorEncryptedFixture = "cursor-safe-storage:v1:djEwXtt8ZL22vcqp1WE+I860/VgrANBr9rXNyrc51nFOiQBb4U5UuohGSg+Uc7Oh2zHdorcHQeKnsPHQirOKIalkWg=="

func TestCursorEncryptedSQLiteSession(t *testing.T) {
	taskHome := t.TempDir()
	empty := func(string) string { return "" }
	path := cursorStateDBPath("darwin", taskHome, empty)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value BLOB)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO ItemTable VALUES (?, ?)", "cursorAuth/accessToken", cursorEncryptedFixture)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	reads := 0
	reader := func(context.Context) ([]byte, error) { reads++; return []byte("synthetic-password"), nil }
	auth, err := loadAutomaticCursorAuth(context.Background(), "darwin", taskHome, empty, os.ReadFile, nil, false, reader)
	require.NoError(t, err)
	require.Equal(t, "user_test", auth.userID)
	require.Equal(t, cursorAuthFixture(), auth.bearer)
	require.Equal(t, 1, reads)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "authentication must not rewrite Cursor credentials")

	_, err = loadAutomaticCursorAuth(context.Background(), "darwin", taskHome, empty, os.ReadFile, nil, false, nil)
	require.ErrorContains(t, err, "Enable Desktop Safe Storage")

	// Existing Agent file fallback remains available when desktop access fails.
	agentPath := cursorAgentAuthFilePath("darwin", taskHome, empty)
	require.NoError(t, os.MkdirAll(filepath.Dir(agentPath), 0700))
	require.NoError(t, os.WriteFile(agentPath, []byte(`{"accessToken":"`+cursorAuthFixture()+`"}`), 0600))
	auth, err = loadAutomaticCursorAuth(context.Background(), "darwin", taskHome, empty, os.ReadFile, nil, false, nil)
	require.NoError(t, err)
	require.Equal(t, "user_test", auth.userID)
}

func TestCursorEncryptedStorageErrorsAreSafe(t *testing.T) {
	for _, tc := range []struct {
		name, stored, platform, password, want string
		readErr                                error
		wantRead                               bool
	}{
		{"wrong version", "cursor-safe-storage:v2:SECRET", "darwin", "", "malformed", nil, false},
		{"bad base64", "cursor-safe-storage:v1:SECRET", "darwin", "", "malformed", nil, false},
		{"bad block", "cursor-safe-storage:v1:djEwAA==", "darwin", "", "malformed", nil, false},
		{"denied", cursorEncryptedFixture, "darwin", "", "denied", errCursorSafeStorageDenied, true},
		{"unavailable", cursorEncryptedFixture, "darwin", "", "unavailable", errors.New("SECRET"), true},
		{"empty password", cursorEncryptedFixture, "darwin", "", "unavailable", nil, true},
		{"decryption failure", cursorEncryptedFixture, "darwin", "wrong-SECRET-password", "could not be decrypted", nil, true},
		{"linux", cursorEncryptedFixture, "linux", "", "only on macOS", nil, false},
		{"windows", cursorEncryptedFixture, "windows", "", "only on macOS", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			reader := func(context.Context) ([]byte, error) { reads++; return []byte(tc.password), tc.readErr }
			for range 3 {
				_, err := cursorDesktopAuth(context.Background(), tc.platform, tc.stored, reader)
				require.ErrorContains(t, err, tc.want)
				require.NotContains(t, err.Error(), "SECRET")
				require.NotContains(t, err.Error(), cursorEncryptedFixture)
				require.Contains(t, err.Error(), "Session cookie")
			}
			require.Equal(t, tc.wantRead, reads > 0)
		})
	}
	// Plain JWTs need neither Keychain opt-in nor a macOS host.
	for _, platform := range []string{"darwin", "linux", "windows"} {
		auth, err := cursorDesktopAuth(context.Background(), platform, cursorAuthFixture(), func(context.Context) ([]byte, error) {
			t.Fatal("plain JWT must not read Safe Storage")
			return nil, nil
		})
		require.NoError(t, err)
		require.Equal(t, "user_test", auth.userID)
	}
}

func TestCursorExplicitCredentialsDoNotReadDesktopKeychain(t *testing.T) {
	for _, token := range []string{cursorAuthFixture(), "cursor-safe-storage:v1:SECRET"} {
		_, err := loadAutomaticCursorAuth(context.Background(), "darwin", t.TempDir(), func(k string) string {
			if k == "CURSOR_AUTH_TOKEN" {
				return token
			}
			return ""
		}, os.ReadFile, nil, false, func(context.Context) ([]byte, error) {
			t.Fatal("explicit token must remain authoritative")
			return nil, nil
		})
		if strings.HasPrefix(token, "cursor-safe-storage:") {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestCursorSafeStorageHelperIsBoundedAndSanitizesFailures(t *testing.T) {
	for _, tc := range []struct {
		output  []byte
		runErr  error
		wantErr bool
	}{
		{[]byte("synthetic-password"), nil, false},
		{[]byte("SECRET"), errors.New("secret stderr=SECRET"), true},
		{nil, context.DeadlineExceeded, true},
		{nil, nil, true},
		{[]byte(strings.Repeat("S", 4097)), nil, true},
	} {
		started := time.Now()
		out, err := readCursorSafeStorageWithRunner(context.Background(), "/plugin binary", func(ctx context.Context, name string, args ...string) ([]byte, error) {
			require.Equal(t, "/plugin binary", name)
			require.Equal(t, []string{cursorSafeStorageHelper}, args)
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.WithinDuration(t, started.Add(5*time.Second), deadline, time.Second)
			return tc.output, tc.runErr
		})
		if tc.wantErr {
			require.Error(t, err)
			require.Empty(t, out)
			require.NotContains(t, err.Error(), "SECRET")
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.output, out)
		}
	}
}
