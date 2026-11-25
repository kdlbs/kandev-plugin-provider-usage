package main

import (
	"runtime"
	"strings"
)

const devinSetupDocs = "https://github.com/steipete/CodexBar/blob/v0.83.0/docs/devin.md"

// Devin's automatic browser import is macOS-only in CodexBar v0.83.0. On Linux
// the CLI reports the unsupported automatic source as a provider error, so
// recognize it as setup guidance while retaining the original diagnostic.
func devinNeedsAuthorization(kind, message string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "not_configured", "not-configured", "not_installed", "not-installed",
		"not_signed_in", "not-signed-in", "unauthenticated", "authentication", "auth_required":
		return true
	}
	m := strings.ToLower(strings.TrimSpace(message))
	for _, marker := range []string{
		"no devin session found",
		"could not read chromium local storage for devin",
		"devin rejected the session token",
		"invalid or expired",
		"no devin organization",
		"devin organization not found",
		"no organizations found for auth1 user",
		"unauthorized",
		"authentication required",
		"auth required",
		"http 401",
		"http 403",
		"selected source requires web support and is only supported on macos",
		"devin bearer token is missing",
		"devin api key is missing",
		"no available fetch strategy for devin",
		"api key not found",
		"missing api key",
		"no credentials",
		"missing credentials",
		"no bearer token",
		"missing bearer token",
	} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

func devinSetupHint(platform string) string {
	switch platform {
	case "darwin":
		return "Sign in to Devin in a supported Chromium browser on the Kandev host and open the organization's Usage & Limits page, then refresh. CodexBar imports Devin's browser session on macOS. For manual authentication, configure a Bearer token and organization in CodexBar. See " + devinSetupDocs
	case "windows":
		return "Configure Devin in the pinned Windows CodexBar port with a bearer value (DEVIN_BEARER_TOKEN or DEVIN_API_KEY) and set DEVIN_ORG to the organization ID, then refresh. See " + devinSetupDocs
	default:
		return "CodexBar browser import for Devin is macOS-only. Configure manual auth in the CodexBar config used by Kandev with cookieSource=manual. Set cookieHeader=Bearer <token>, or provide DEVIN_BEARER_TOKEN or DEVIN_AUTHORIZATION; the environment token does not require cookieHeader. Set workspaceID=<organization ID>, DEVIN_ORGANIZATION, or DEVIN_ORG. Then refresh. See " + devinSetupDocs
	}
}

func (p *plugin) addDevinSetup(report *AllProvidersReport) {
	for i := range report.Unavailable {
		failure := &report.Unavailable[i]
		if failure.Provider != "devin" || !devinNeedsAuthorization(failure.Kind, failure.Message) {
			continue
		}
		failure.Kind = "not_configured"
		failure.Code = "devin_authorization"
		failure.Setup = &providerSetup{Hint: devinSetupHint(runtime.GOOS)}
	}
}
