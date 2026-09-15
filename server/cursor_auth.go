package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite" // read Cursor's local auth without a system sqlite3 or CGO
)

const cursorCookieSetting = "cursor_cookie_header"
const cursorDetailHint = "For Cursor details, sign in to the Cursor app on this host or set Cursor · Session cookie in plugin settings."

type cursorAuth struct {
	cookie string
	bearer string
	userID string
}

// Credentials stay in the backend. We read the existing session on every poll;
// the plugin never modifies Cursor's database or refreshes its login in place.
// Use only configured cookies and the local app database: background Keychain
// reads can repeatedly interrupt the user with macOS permission prompts.
func loadCursorAuth(ctx context.Context, cfg map[string]any) (cursorAuth, error) {
	if manual := trimmedString(cfg[cursorCookieSetting]); manual != "" {
		return cursorCookieAuth(manual)
	}
	taskHome, err := os.UserHomeDir()
	if err != nil {
		return cursorAuth{}, errors.New(cursorDetailHint)
	}
	configPath := cursorCodexbarConfigPath(taskHome, os.Getenv)
	if raw, err := os.ReadFile(configPath); err == nil {
		if auth, selected, err := cursorConfiguredAuth(raw); selected || err != nil {
			return auth, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return cursorAuth{}, errors.New("Cannot read CodexBar configuration for Cursor details.")
	}
	path := cursorStateDBPath(runtime.GOOS, taskHome, os.Getenv)
	if token, err := readCursorAccessToken(ctx, path); err == nil && token != "" {
		return cursorBearerAuth(token)
	}
	return cursorAuth{}, errors.New(cursorDetailHint)
}

func cursorCodexbarConfigPath(taskHome string, getenv func(string) string) string {
	if path := strings.TrimSpace(getenv("CODEXBAR_CONFIG")); path != "" {
		return path
	}
	base := strings.TrimSpace(getenv("XDG_CONFIG_HOME"))
	if !filepath.IsAbs(base) {
		base = filepath.Join(taskHome, ".config")
	}
	path := filepath.Join(base, "codexbar", "config.json")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		legacy := filepath.Join(taskHome, ".codexbar", "config.json")
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return path
}

func cursorConfiguredAuth(raw []byte) (cursorAuth, bool, error) {
	var config struct {
		Providers []struct {
			ID           string `json:"id"`
			CookieSource string `json:"cookieSource"`
			CookieHeader string `json:"cookieHeader"`
		} `json:"providers"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return cursorAuth{}, true, errors.New("Cannot parse CodexBar configuration for Cursor details.")
	}
	for _, provider := range config.Providers {
		if provider.ID != "cursor" {
			continue
		}
		switch provider.CookieSource {
		case "off":
			return cursorAuth{}, true, errors.New("Cursor details are disabled by CodexBar's cookie-source setting.")
		case "manual":
			auth, err := cursorCookieAuth(provider.CookieHeader)
			return auth, true, err
		}
	}
	return cursorAuth{}, false, nil
}

func cursorStateDBPath(platform, taskHome string, getenv func(string) string) string {
	var base string
	switch platform {
	case "darwin":
		base = filepath.Join(taskHome, "Library", "Application Support")
	case "windows":
		base = getenv("APPDATA")
		if base == "" {
			base = filepath.Join(taskHome, "AppData", "Roaming")
		}
	default:
		base = getenv("XDG_CONFIG_HOME")
		if !filepath.IsAbs(base) {
			base = filepath.Join(taskHome, ".config")
		}
	}
	return filepath.Join(base, "Cursor", "User", "globalStorage", "state.vscdb")
}

func readCursorAccessToken(ctx context.Context, path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro&_pragma=busy_timeout(250)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return "", errors.New("Cannot open Cursor's local session.")
	}
	defer db.Close()
	var token string
	err = db.QueryRowContext(ctx, "SELECT value FROM ItemTable WHERE key = ? LIMIT 1", "cursorAuth/accessToken").Scan(&token)
	if err != nil {
		return "", errors.New("Cannot read Cursor's local session.")
	}
	return strings.TrimSpace(token), nil
}

func cursorBearerAuth(token string) (cursorAuth, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return cursorAuth{}, errors.New(cursorDetailHint)
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	var payload struct {
		Sub string `json:"sub"`
	}
	if err != nil || json.Unmarshal(data, &payload) != nil {
		return cursorAuth{}, errors.New(cursorDetailHint)
	}
	subject := strings.Split(payload.Sub, "|")
	id := subject[len(subject)-1]
	if id == "" || strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r))
	}) >= 0 {
		return cursorAuth{}, errors.New(cursorDetailHint)
	}
	return cursorAuth{cookie: "WorkosCursorSessionToken=" + id + "%3A%3A" + token, bearer: token, userID: id}, nil
}

func cursorCookieAuth(raw string) (cursorAuth, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "cookie:") {
		raw = strings.TrimSpace(raw[7:])
	}
	if strings.ContainsAny(raw, "\r\n") {
		return cursorAuth{}, errors.New("Cursor's session cookie must be a single Cookie request header.")
	}
	auth := cursorAuth{cookie: raw}
	found := false
	for _, part := range strings.Split(raw, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || value == "" {
			continue
		}
		switch name {
		case "WorkosCursorSessionToken":
			found = true
			decoded, err := url.PathUnescape(value)
			if err != nil {
				continue
			}
			id, token, ok := strings.Cut(decoded, "::")
			if ok {
				auth.userID = id
				if bearer, err := cursorBearerAuth(token); err == nil && bearer.userID == id {
					auth.bearer = bearer.bearer
				}
			}
		case "__Secure-next-auth.session-token", "next-auth.session-token":
			found = true
		}
	}
	if !found {
		return cursorAuth{}, errors.New("Cursor session cookie is missing. Copy the Cookie request header from a signed-in cursor.com dashboard.")
	}
	return auth, nil
}
