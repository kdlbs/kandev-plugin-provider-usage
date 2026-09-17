package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite" // read Cursor's local auth without a system sqlite3 or CGO
)

const cursorCookieSetting = "cursor_cookie_header"
const cursorAgentKeychainSetting = "cursor_agent_keychain"
const cursorDetailHint = "For Cursor details, sign in to Cursor Desktop or Cursor Agent CLI on this host, or set Cursor · Session cookie in plugin settings."

const cursorAgentKeychainTimeout = 5 * time.Second

type cursorSecretRunner func(context.Context, string, ...string) ([]byte, error)

type cursorAuth struct {
	cookie       string
	bearer       string
	userID       string
	alternates   []cursorAuth
	alternateErr error
}

// Credentials stay in the backend. We read the existing session on every poll;
// the plugin never modifies Cursor's database or refreshes its login in place.
// Configured credentials select one account. Automatic local credentials can
// include both Desktop and Agent CLI sessions, so a rejected stale session can
// fall through to the other without exposing either token to the UI.
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

	allowAgentKeychain := strings.EqualFold(trimmedString(cfg[cursorAgentKeychainSetting]), "on")
	return loadAutomaticCursorAuth(ctx, runtime.GOOS, taskHome, os.Getenv, os.ReadFile, runCursorSecret, allowAgentKeychain)
}

func loadAutomaticCursorAuth(
	ctx context.Context,
	platform, taskHome string,
	getenv func(string) string,
	readFile func(string) ([]byte, error),
	run cursorSecretRunner,
	allowAgentKeychain bool,
) (cursorAuth, error) {
	// CURSOR_AUTH_TOKEN is the Agent CLI's explicit, supported bearer override.
	// Treat it like the explicit cookie above: an invalid value must not silently
	// switch to another locally signed-in account.
	if token := strings.TrimSpace(getenv("CURSOR_AUTH_TOKEN")); token != "" {
		auth, err := cursorBearerAuth(token)
		if err != nil {
			return cursorAuth{}, errors.New("Cursor Agent CLI's CURSOR_AUTH_TOKEN is invalid. Sign in again or clear it.")
		}
		return auth, nil
	}

	candidates := make([]cursorAuth, 0, 2)
	path := cursorStateDBPath(platform, taskHome, getenv)
	if token, err := readCursorAccessToken(ctx, path); err == nil && token != "" {
		if auth, err := cursorBearerAuth(token); err == nil {
			candidates = append(candidates, auth)
		}
	}
	agentToken, agentFound, agentErr := readCursorAgentAccessToken(
		ctx, platform, taskHome, getenv, readFile, run, allowAgentKeychain,
	)
	if agentErr == nil && agentToken != "" {
		if auth, err := cursorBearerAuth(agentToken); err == nil {
			candidates = append(candidates, auth)
		} else {
			agentErr = errors.New("Cursor Agent CLI's saved session is invalid. Run `agent login` again.")
		}
	}
	if len(candidates) > 0 {
		auth := combineCursorAuth(candidates)
		if agentFound && agentErr != nil {
			auth.alternateErr = agentErr
		}
		return auth, nil
	}
	if agentFound && agentErr != nil {
		return cursorAuth{}, agentErr
	}
	return cursorAuth{}, errors.New(cursorDetailHint)
}

func combineCursorAuth(candidates []cursorAuth) cursorAuth {
	unique := make([]cursorAuth, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate.alternates = nil
		key := candidate.cookie + "\x00" + candidate.bearer
		if key == "\x00" || seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, candidate)
	}
	if len(unique) == 0 {
		return cursorAuth{}
	}
	primary := unique[0]
	primary.alternates = append([]cursorAuth(nil), unique[1:]...)
	return primary
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

// cursorAgentAuthFilePath mirrors the current Agent CLI credential file. The
// CLI uses macOS Keychain by default, with this file as its explicit file-store
// location; Linux and Windows use the file directly.
func cursorAgentAuthFilePath(platform, taskHome string, getenv func(string) string) string {
	switch platform {
	case "darwin":
		return filepath.Join(taskHome, ".cursor", "auth.json")
	case "windows":
		base := strings.TrimSpace(getenv("APPDATA"))
		if base == "" {
			base = filepath.Join(taskHome, "AppData", "Roaming")
		}
		return filepath.Join(base, "Cursor", "auth.json")
	default:
		base := strings.TrimSpace(getenv("XDG_CONFIG_HOME"))
		if base == "" {
			base = filepath.Join(taskHome, ".config")
		}
		return filepath.Join(base, "cursor", "auth.json")
	}
}

func readCursorAgentAuthFile(path string, readFile func(string) ([]byte, error)) (string, bool, error) {
	raw, err := readFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", true, errors.New("Cannot read Cursor Agent CLI's saved session. Run `agent login` again.")
	}
	if len(raw) > 2<<20 {
		return "", true, errors.New("Cursor Agent CLI's saved session is too large. Run `agent login` again.")
	}
	var saved struct {
		AccessToken string `json:"accessToken"`
	}
	if json.Unmarshal(raw, &saved) != nil || strings.TrimSpace(saved.AccessToken) == "" {
		return "", true, errors.New("Cursor Agent CLI's saved session is invalid. Run `agent login` again.")
	}
	return strings.TrimSpace(saved.AccessToken), true, nil
}

// readCursorAgentAccessToken reads only the access token written by `agent
// login`. Refresh remains the CLI's responsibility. On macOS, checking for the
// item without -w avoids prompting users who never signed in; the secret is
// requested only when the expected item exists.
func readCursorAgentAccessToken(
	ctx context.Context,
	platform, taskHome string,
	getenv func(string) string,
	readFile func(string) ([]byte, error),
	run cursorSecretRunner,
	allowKeychain bool,
) (string, bool, error) {
	path := cursorAgentAuthFilePath(platform, taskHome, getenv)
	if platform != "darwin" {
		return readCursorAgentAuthFile(path, readFile)
	}

	switch strings.ToLower(strings.TrimSpace(getenv("AGENT_CLI_CREDENTIAL_STORE"))) {
	case "memory":
		return "", false, nil
	case "file":
		return readCursorAgentAuthFile(path, readFile)
	}

	if !allowKeychain {
		return readCursorAgentAuthFile(path, readFile)
	}

	if run == nil {
		return "", true, errors.New("Cannot inspect Cursor Agent CLI's macOS Keychain session.")
	}
	keychainCtx, cancel := context.WithTimeout(ctx, cursorAgentKeychainTimeout)
	_, presentErr := run(keychainCtx, "/usr/bin/security", "find-generic-password", "-s", "cursor-access-token", "-a", "cursor-user")
	cancel()
	if presentErr == nil {
		keychainCtx, cancel = context.WithTimeout(ctx, cursorAgentKeychainTimeout)
		out, err := run(keychainCtx, "/usr/bin/security", "find-generic-password", "-s", "cursor-access-token", "-a", "cursor-user", "-w")
		cancel()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			return "", true, errors.New("Cannot read Cursor Agent CLI's macOS Keychain session. Run `agent login` again and allow access.")
		}
		return strings.TrimSpace(string(out)), true, nil
	}
	type exitCoder interface{ ExitCode() int }
	var exitErr exitCoder
	if !errors.As(presentErr, &exitErr) || exitErr.ExitCode() != 44 {
		return "", true, errors.New("Cannot inspect Cursor Agent CLI's macOS Keychain session. Unlock Keychain and try again.")
	}
	return readCursorAgentAuthFile(path, readFile)
}

func runCursorSecret(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
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
