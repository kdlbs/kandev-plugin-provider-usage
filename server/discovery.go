package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

const configKeyDisabledProviders = "disabled_providers"
const webhookKeyDiscovery = "discovery"

// Settings must not join a potentially slow usage poll just to find out which
// controls to render. This path reads local evidence and the existing snapshot;
// it never resolves/downloads CodexBar, executes a CLI or contacts a provider.
func (p *plugin) discoveryWebhook(ctx context.Context, method string) *pluginsdk.WebhookResponse {
	if method != http.MethodGet {
		return jsonResponse(http.StatusMethodNotAllowed, []byte(`{"error":"Use GET to scan local providers."}`))
	}
	snapshot, _ := p.currentSnapshot()
	if snapshot == nil {
		snapshot = &AllProvidersReport{}
	}
	report := struct {
		DetectedProviders []detectedProvider `json:"detected_providers"`
		ScannedAt         string             `json:"scanned_at"`
	}{p.discoverProviders(p.config(ctx), snapshot), p.now().UTC().Format(time.RFC3339)}
	return jsonResponse(http.StatusOK, marshalOr(report, `{"error":"Cannot scan providers."}`))
}

// Presence is independent of usage availability: an installed but signed-out
// provider still needs its settings, while a failed speculative CLI probe is
// not evidence that the provider exists on this machine.
type detectedProvider struct {
	Provider string `json:"provider"`
	Via      string `json:"via"`
}

type providerScanner struct {
	home     string
	getenv   func(string) string
	lookPath func(string) (string, error)
	exists   func(string) bool
}

func scanLocalProviders(cfg map[string]any) []detectedProvider {
	taskHome, _ := os.UserHomeDir()
	scanner := providerScanner{
		home: taskHome, getenv: os.Getenv, lookPath: exec.LookPath,
		exists: func(path string) bool { _, err := os.Stat(path); return err == nil },
	}
	return scanner.scan(cfg)
}

func (s providerScanner) scan(cfg map[string]any) []detectedProvider {
	configHome := s.getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(configHome) {
		configHome = filepath.Join(s.home, ".config")
	}
	codexHome := s.getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(s.home, ".codex")
	}
	appData := s.getenv("APPDATA")
	if appData == "" {
		appData = filepath.Join(s.home, "AppData", "Roaming")
	}
	cursorConfigDir := strings.TrimSpace(s.getenv("CURSOR_CONFIG_DIR"))
	if cursorConfigDir == "" {
		cursorConfigDir = filepath.Join(s.home, ".cursor")
		if strings.TrimSpace(s.getenv("XDG_CONFIG_HOME")) != "" {
			cursorConfigDir = filepath.Join(configHome, "cursor")
		}
	}
	rules := []struct {
		id       string
		commands []string
		paths    []string
	}{
		{"claude", []string{"claude", "claude-agent-acp"}, []string{filepath.Join(s.home, ".claude", ".credentials.json"), filepath.Join(s.home, ".claude.json")}},
		{"codex", []string{"codex", "codex-acp"}, []string{filepath.Join(codexHome, "auth.json"), filepath.Join(codexHome, "config.toml")}},
		{"gemini", []string{"gemini"}, []string{filepath.Join(s.home, ".gemini", "oauth_creds.json"), filepath.Join(s.home, ".gemini", "settings.json")}},
		{"copilot", []string{"copilot"}, []string{filepath.Join(configHome, "github-copilot", "apps.json"), filepath.Join(configHome, "github-copilot", "hosts.json"), filepath.Join(s.home, ".copilot", "config.json")}},
		{"cursor", []string{"cursor", "cursor-agent", "cursor-agent-acp"}, []string{
			"/Applications/Cursor.app", filepath.Join(s.home, "Applications", "Cursor.app"),
			filepath.Join(s.home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"),
			filepath.Join(configHome, "Cursor", "User", "globalStorage", "state.vscdb"),
			filepath.Join(appData, "Cursor", "User", "globalStorage", "state.vscdb"),
			filepath.Join(cursorConfigDir, "cli-config.json"),
			filepath.Join(s.home, ".cursor", "auth.json"),
			filepath.Join(configHome, "cursor", "auth.json"),
			filepath.Join(appData, "Cursor", "auth.json"),
		}},
		{"grok", []string{"grok"}, nil},
		{"opencodego", []string{"opencode"}, []string{filepath.Join(configHome, "opencode", "opencode.json"), filepath.Join(s.home, ".local", "share", "opencode", "auth.json")}},
		{"amp", []string{"amp", "amp-acp"}, []string{filepath.Join(configHome, "amp", "settings.json")}},
		{"augment", []string{"auggie"}, nil},
	}
	found := map[string]string{}
	for _, rule := range rules {
		for _, command := range rule.commands {
			if _, err := s.lookPath(command); err == nil {
				found[rule.id] = "cli"
				break
			}
		}
		if found[rule.id] != "" || s.home == "" {
			continue
		}
		for _, path := range rule.paths {
			if s.exists(path) {
				found[rule.id] = "configuration"
				if strings.HasSuffix(path, ".app") {
					found[rule.id] = "app"
				}
				break
			}
		}
	}
	if trimmedString(cfg[cursorCookieSetting]) != "" || cursorTeamConfigured(cfg) {
		found["cursor"] = "configuration"
	}
	if trimmedString(cfg[configKeyAugmentToken]) != "" || trimmedString(cfg[configKeyAugmentEmail]) != "" {
		found["augment"] = "configuration"
	}
	return sortedDetectedProviders(found)
}

// Explicit CodexBar credentials also count as a connection on this host,
// including providers outside our local-CLI catalog. Only IDs leave this read.
func configuredCodexbarProviders() []detectedProvider {
	taskHome, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	file, err := os.Open(cursorCodexbarConfigPath(taskHome, os.Getenv))
	if err != nil {
		return nil
	}
	defer file.Close()
	var config struct {
		Providers []struct {
			ID           string `json:"id"`
			CookieHeader string `json:"cookieHeader"`
			APIKey       string `json:"apiKey"`
			Token        string `json:"token"`
		} `json:"providers"`
	}
	if json.NewDecoder(io.LimitReader(file, 2<<20)).Decode(&config) != nil {
		return nil
	}
	found := map[string]string{}
	for _, provider := range config.Providers {
		id := strings.ToLower(strings.TrimSpace(provider.ID))
		if id != "" && (strings.TrimSpace(provider.CookieHeader) != "" || strings.TrimSpace(provider.APIKey) != "" || strings.TrimSpace(provider.Token) != "") {
			found[id] = "configuration"
		}
	}
	return sortedDetectedProviders(found)
}

func sortedDetectedProviders(found map[string]string) []detectedProvider {
	providers := make([]detectedProvider, 0, len(found))
	for id, via := range found {
		if alias := providerAliases[id]; alias != "" {
			id = alias
		}
		providers = append(providers, detectedProvider{Provider: id, Via: via})
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Provider < providers[j].Provider })
	return providers
}

func (p *plugin) discoverProviders(cfg map[string]any, report *AllProvidersReport) []detectedProvider {
	found := map[string]string{}
	// A paused connection retains its controls so it can be enabled again.
	for _, id := range strings.Split(trimmedString(cfg[configKeyDisabledProviders]), ",") {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
			found[id] = "configuration"
		}
	}
	if p.scanProviders != nil {
		for _, provider := range p.scanProviders(cfg) {
			found[provider.Provider] = provider.Via
		}
	}
	for _, provider := range report.Providers {
		if found[provider.Provider] == "" {
			found[provider.Provider] = "usage"
		}
	}
	return sortedDetectedProviders(found)
}

func providerDisabled(cfg map[string]any, id string) bool {
	if id == "" {
		return false
	}
	for _, value := range strings.Split(trimmedString(cfg[configKeyDisabledProviders]), ",") {
		value = strings.ToLower(strings.TrimSpace(value))
		if alias := providerAliases[value]; alias != "" {
			value = alias
		}
		if value == id {
			return true
		}
	}
	return false
}
