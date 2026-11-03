package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

const cursorSafeStorageHelper = "--cursor-safe-storage-secret"
const cursorSafeStorageHint = " Use Cursor · Session cookie, or choose and save Account usage (automatic)."

var (
	errCursorSafeStorageDenied  = errors.New("Cursor desktop Safe Storage access was denied or requires interaction. Background reads never prompt." + cursorSafeStorageHint)
	errCursorSafeStorageMissing = errors.New("Cursor desktop Safe Storage is unavailable on this host. Unlock the login Keychain or use the Cursor desktop account on this host." + cursorSafeStorageHint)
)

type cursorSafeStorageReader func(context.Context) ([]byte, error)

// The helper isolates Security.framework's process-wide no-interaction policy
// and bounds native Keychain calls. Only its private stdout pipe carries a secret.
func readCursorSafeStoragePassword(ctx context.Context) ([]byte, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, errCursorSafeStorageMissing
	}
	return readCursorSafeStorageWithRunner(ctx, executable, runCursorSecret)
}

func readCursorSafeStorageWithRunner(ctx context.Context, executable string, run cursorSecretRunner) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := run(ctx, executable, cursorSafeStorageHelper)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			return nil, errCursorSafeStorageDenied
		}
		return nil, errCursorSafeStorageMissing
	}
	if len(out) == 0 || len(out) > 4096 || !utf8.Valid(out) {
		return nil, errCursorSafeStorageMissing
	}
	return out, nil
}

// Electron OSCrypt on macOS: v10 + AES-128-CBC, PBKDF2-SHA1 with
// saltysalt/1003 iterations and a 16-space IV. Match CodexBar v0.83.0's
// ChromiumSafeStorage implementation; validate the JWT only after decryption.
func cursorDesktopAuth(ctx context.Context, platform, stored string, read cursorSafeStorageReader) (cursorAuth, error) {
	if !strings.HasPrefix(stored, "cursor-safe-storage:") {
		return cursorBearerAuth(stored)
	}
	invalid := errors.New("Cursor desktop encrypted session is malformed or could not be decrypted." + cursorSafeStorageHint)
	const prefix = "cursor-safe-storage:v1:"
	if !strings.HasPrefix(stored, prefix) || len(stored) > 2<<20 {
		return cursorAuth{}, invalid
	}
	encrypted, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil || len(encrypted) <= 3 || !bytes.HasPrefix(encrypted, []byte("v10")) || (len(encrypted)-3)%aes.BlockSize != 0 {
		return cursorAuth{}, invalid
	}
	if platform != "darwin" {
		return cursorAuth{}, errors.New("Cursor desktop encrypted storage is supported only on macOS." + cursorSafeStorageHint)
	}
	if read == nil {
		return cursorAuth{}, errors.New("Cursor desktop session is encrypted. Enable Desktop Safe Storage below for noninteractive macOS Keychain access. Agent CLI Keychain is a separate credential source." + cursorSafeStorageHint)
	}
	password, err := read(ctx)
	if err != nil {
		if errors.Is(err, errCursorSafeStorageDenied) {
			return cursorAuth{}, errCursorSafeStorageDenied
		}
		return cursorAuth{}, errCursorSafeStorageMissing
	}
	if len(password) == 0 || len(password) > 4096 || !utf8.Valid(password) {
		return cursorAuth{}, errCursorSafeStorageMissing
	}
	key, err := pbkdf2.Key(sha1.New, string(password), []byte("saltysalt"), 1003, 16)
	if err != nil {
		return cursorAuth{}, invalid
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return cursorAuth{}, invalid
	}
	plain := make([]byte, len(encrypted)-3)
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte{0x20}, aes.BlockSize)).CryptBlocks(plain, encrypted[3:])
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > aes.BlockSize || !bytes.Equal(plain[len(plain)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		return cursorAuth{}, invalid
	}
	plain = plain[:len(plain)-padding]
	if !utf8.Valid(plain) {
		return cursorAuth{}, invalid
	}
	auth, err := cursorBearerAuth(strings.TrimSpace(string(plain)))
	if err != nil {
		return cursorAuth{}, invalid
	}
	return auth, nil
}
