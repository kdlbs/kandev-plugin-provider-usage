package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func cursorAuthFixture() string {
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"auth0|user_test"}`)) + ".signature"
}

func cursorAuthFixtureFor(user string) string {
	payload, _ := json.Marshal(map[string]string{"sub": "auth0|" + user})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

type cursorTestExitError int

func (e cursorTestExitError) Error() string { return "security command failed" }
func (e cursorTestExitError) ExitCode() int { return int(e) }

func TestCursorAuthCookieAndBearerSources(t *testing.T) {
	token := cursorAuthFixture()
	auth, err := cursorBearerAuth(token)
	require.NoError(t, err)
	require.Equal(t, "user_test", auth.userID)
	require.Equal(t, "WorkosCursorSessionToken=user_test%3A%3A"+token, auth.cookie)
	parsed, err := cursorCookieAuth("Cookie: " + auth.cookie + "; preference=on")
	require.NoError(t, err)
	require.Equal(t, auth.bearer, parsed.bearer)
	require.Equal(t, auth.userID, parsed.userID)

	for _, input := range []string{"", "unrelated=value", "Cookie: ", "WorkosCursorSessionToken=value\r\nX-Sent: secret"} {
		_, err := cursorCookieAuth(input)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
	parsed, err = cursorCookieAuth("next-auth.session-token=opaque-cookie")
	require.NoError(t, err)
	require.Empty(t, parsed.bearer, "opaque cookies can use REST but never become RPC bearer tokens")
	_, err = cursorBearerAuth("header." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"auth0|bad;user"}`)) + ".signature")
	require.Error(t, err)
}

func TestCursorConfigHonorsExplicitSelection(t *testing.T) {
	for _, source := range []string{"manual", "off"} {
		_, selected, err := cursorConfiguredAuth([]byte(`{"providers":[{"id":"cursor","cookieSource":"` + source + `"}]}`))
		require.True(t, selected)
		require.Error(t, err, "explicit off/empty manual must never fall back to another account")
	}
	_, selected, err := cursorConfiguredAuth([]byte(`{"providers":[{"id":"cursor","cookieSource":"auto","cookieHeader":"WorkosCursorSessionToken=old"}]}`))
	require.NoError(t, err)
	require.False(t, selected, "an old manual cookie must stay passive in automatic mode")
	_, selected, err = cursorConfiguredAuth([]byte(`{"providers":"credential=DO-NOT-EXPOSE"}`))
	require.True(t, selected)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "DO-NOT-EXPOSE")

	path := filepath.Join(t.TempDir(), "codexbar.json")
	t.Setenv("CODEXBAR_CONFIG", path)
	raw, err := json.Marshal(map[string]any{"providers": []any{map[string]any{"id": "cursor", "cookieSource": "manual", "cookieHeader": "WorkosCursorSessionToken=saved-session"}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))
	auth, err := loadCursorAuth(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "WorkosCursorSessionToken=saved-session", auth.cookie)
	auth, err = loadCursorAuth(context.Background(), map[string]any{cursorCookieSetting: "WorkosCursorSessionToken=plugin-session"})
	require.NoError(t, err)
	require.Equal(t, "WorkosCursorSessionToken=plugin-session", auth.cookie)
	_, err = loadCursorAuth(context.Background(), map[string]any{cursorCookieSetting: "invalid"})
	require.Error(t, err, "bad explicit settings cannot silently select ambient credentials")
}

func TestCursorConfigPaths(t *testing.T) {
	dir := t.TempDir()
	empty := func(string) string { return "" }
	require.Equal(t, filepath.Join(dir, ".config", "codexbar", "config.json"), cursorCodexbarConfigPath(dir, empty))
	legacy := filepath.Join(dir, ".codexbar", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0700))
	require.NoError(t, os.WriteFile(legacy, []byte(`{}`), 0600))
	require.Equal(t, legacy, cursorCodexbarConfigPath(dir, empty))
	xdg := filepath.Join(dir, "xdg")
	xdgConfig := filepath.Join(xdg, "codexbar", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(xdgConfig), 0700))
	require.NoError(t, os.WriteFile(xdgConfig, []byte(`{}`), 0600))
	require.Equal(t, xdgConfig, cursorCodexbarConfigPath(dir, func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			return xdg
		}
		return ""
	}))
	require.Equal(t, "override.json", cursorCodexbarConfigPath(dir, func(key string) string {
		if key == "CODEXBAR_CONFIG" {
			return "override.json"
		}
		return ""
	}))
	for _, platform := range []string{"linux", "darwin", "windows"} {
		path := cursorStateDBPath(platform, dir, empty)
		require.Contains(t, path, filepath.Join("Cursor", "User", "globalStorage", "state.vscdb"))
	}
	require.Equal(t, filepath.Join(xdg, "Cursor", "User", "globalStorage", "state.vscdb"), cursorStateDBPath("linux", dir, func(string) string { return xdg }))
	require.Equal(t, filepath.Join(dir, ".config", "cursor", "auth.json"), cursorAgentAuthFilePath("linux", dir, empty))
	require.Equal(t, filepath.Join(xdg, "cursor", "auth.json"), cursorAgentAuthFilePath("linux", dir, func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			return xdg
		}
		return ""
	}))
	require.Equal(t, filepath.Join(dir, ".cursor", "auth.json"), cursorAgentAuthFilePath("darwin", dir, empty))
	require.Equal(t, filepath.Join(dir, "AppData", "Roaming", "Cursor", "auth.json"), cursorAgentAuthFilePath("windows", dir, empty))
	require.Equal(t, filepath.Join("/custom/roaming", "Cursor", "auth.json"), cursorAgentAuthFilePath("windows", dir, func(key string) string {
		if key == "APPDATA" {
			return "/custom/roaming"
		}
		return ""
	}))
}

func TestCursorAgentAuthFileReadsOnlyAccessToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	token := cursorAuthFixture()
	raw, err := json.Marshal(map[string]string{
		"accessToken":  token,
		"refreshToken": "DO-NOT-EXPOSE",
		"apiKey":       "ALSO-PRIVATE",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))

	got, found, err := readCursorAgentAuthFile(path, os.ReadFile)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, token, got)

	require.NoError(t, os.WriteFile(path, []byte(`{"accessToken":"","refreshToken":"DO-NOT-EXPOSE"}`), 0600))
	_, found, err = readCursorAgentAuthFile(path, os.ReadFile)
	require.True(t, found)
	require.ErrorContains(t, err, "saved session is invalid")
	require.NotContains(t, err.Error(), "DO-NOT-EXPOSE")

	_, found, err = readCursorAgentAuthFile(filepath.Join(t.TempDir(), "missing.json"), os.ReadFile)
	require.NoError(t, err)
	require.False(t, found)
}

func TestCursorAutomaticAuthUsesAgentCLIAndRetainsDesktopFallback(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	env := func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			return xdg
		}
		return ""
	}
	agentPath := cursorAgentAuthFilePath("linux", home, env)
	require.NoError(t, os.MkdirAll(filepath.Dir(agentPath), 0700))
	agentToken := cursorAuthFixtureFor("agent_user")
	require.NoError(t, os.WriteFile(agentPath, []byte(`{"accessToken":"`+agentToken+`","refreshToken":"unused"}`), 0600))

	auth, err := loadAutomaticCursorAuth(context.Background(), "linux", home, env, os.ReadFile, nil, false)
	require.NoError(t, err)
	require.Equal(t, "agent_user", auth.userID)
	require.Empty(t, auth.alternates)

	desktopPath := cursorStateDBPath("linux", home, env)
	require.NoError(t, os.MkdirAll(filepath.Dir(desktopPath), 0700))
	db, err := sql.Open("sqlite", desktopPath)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value BLOB)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO ItemTable (key,value) VALUES (?,?)", "cursorAuth/accessToken", cursorAuthFixtureFor("desktop_user"))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	auth, err = loadAutomaticCursorAuth(context.Background(), "linux", home, env, os.ReadFile, nil, false)
	require.NoError(t, err)
	require.Equal(t, "desktop_user", auth.userID)
	require.Len(t, auth.alternates, 1)
	require.Equal(t, "agent_user", auth.alternates[0].userID)

	require.NoError(t, os.WriteFile(agentPath, []byte(`{"accessToken":"","refreshToken":"unused"}`), 0600))
	auth, err = loadAutomaticCursorAuth(context.Background(), "linux", home, env, os.ReadFile, nil, false)
	require.NoError(t, err, "a valid Desktop session remains usable")
	require.Equal(t, "desktop_user", auth.userID)
	require.Empty(t, auth.alternates)
	require.ErrorContains(t, auth.alternateErr, "saved session is invalid")
}

func TestCursorAgentExplicitTokenDoesNotFallBack(t *testing.T) {
	home := t.TempDir()
	env := func(key string) string {
		if key == "CURSOR_AUTH_TOKEN" {
			return "invalid-token"
		}
		return ""
	}
	_, err := loadAutomaticCursorAuth(context.Background(), "linux", home, env, os.ReadFile, nil, false)
	require.ErrorContains(t, err, "CURSOR_AUTH_TOKEN is invalid")
}

func TestCursorAgentMacOSKeychainLookup(t *testing.T) {
	token := cursorAuthFixtureFor("agent_user")
	fileAuth := func(string) ([]byte, error) { return []byte(`{"accessToken":"` + token + `"}`), nil }
	got, found, err := readCursorAgentAccessToken(context.Background(), "darwin", "/home/test", func(string) string { return "" }, fileAuth, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("a readable auth file must not touch Keychain when Keychain access is disabled")
		return nil, nil
	}, false)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, token, got)

	var calls [][]string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if args[len(args)-1] == "-w" {
			return []byte(token + "\n"), nil
		}
		return []byte("attributes only"), nil
	}
	missing := func(string) ([]byte, error) { return nil, os.ErrNotExist }
	calls = nil
	_, found, err = readCursorAgentAccessToken(context.Background(), "darwin", "/home/test", func(string) string { return "" }, missing, run, false)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, calls, "disabled Keychain access must not invoke the security tool")

	calls = nil
	got, found, err = readCursorAgentAccessToken(context.Background(), "darwin", "/home/test", func(string) string { return "" }, missing, run, true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, token, got)
	require.Len(t, calls, 2)
	require.Equal(t, "/usr/bin/security", calls[0][0])
	require.NotContains(t, calls[0], "-w", "presence check must not request the secret")
	require.Contains(t, calls[1], "-w")

	notFound := func(context.Context, string, ...string) ([]byte, error) {
		return nil, cursorTestExitError(44)
	}
	_, found, err = readCursorAgentAccessToken(context.Background(), "darwin", "/home/test", func(string) string { return "" }, missing, notFound, true)
	require.NoError(t, err, "an absent Keychain item may fall back to the file store")
	require.False(t, found)

	denied := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("credential=DO-NOT-EXPOSE")
	}
	got, found, err = readCursorAgentAccessToken(context.Background(), "darwin", "/home/test", func(string) string { return "" }, fileAuth, denied, true)
	require.ErrorContains(t, err, "Cannot inspect")
	require.True(t, found)
	require.Empty(t, got, "a stale file must not replace a Keychain session after an inspection failure")
	require.NotContains(t, err.Error(), "DO-NOT-EXPOSE")
}

func TestCursorSQLiteReadsOnlyAccessTokenWithoutCreatingMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state # &.vscdb")
	_, err := readCursorAccessToken(context.Background(), path)
	require.Error(t, err)
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value BLOB)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO ItemTable (key,value) VALUES (?,?), (?,?)", "cursorAuth/accessToken", []byte(cursorAuthFixture()), "cursorAuth/refreshToken", "must-not-be-read")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	token, err := readCursorAccessToken(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, cursorAuthFixture(), token)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
