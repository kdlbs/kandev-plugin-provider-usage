package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func cursorAuthFixture() string {
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"auth0|user_test"}`)) + ".signature"
}

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
