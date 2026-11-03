package main

import (
	"context"
	"runtime"
	"strings"
	"time"
)

type providerSetup struct {
	Command string `json:"command,omitempty"`
	Shell   string `json:"shell,omitempty"`
	Hint    string `json:"hint"`
}

// These are authorization failures, not network, service, or unsupported-source
// errors. A local Copilot CLI installation does not authorize CodexBar.
func copilotNeedsAuthorization(message string) bool {
	m := strings.TrimSpace(strings.ToLower(message))
	return m == "no available fetch strategy for copilot." || m == "no available fetch strategy for copilot" ||
		strings.Contains(m, "unauthorized") || strings.Contains(m, "http 401") ||
		strings.Contains(m, "no copilot token") || strings.Contains(m, "copilot token is missing")
}

func (p *plugin) addCopilotSetup(ctx context.Context, cmd resolvedCommand, report *AllProvidersReport) {
	for i := range report.Unavailable {
		failure := &report.Unavailable[i]
		if failure.Provider != "copilot" || !copilotNeedsAuthorization(failure.Message) {
			continue
		}
		failure.Code = "copilot_authorization"
		setup := &providerSetup{Hint: "Copilot CLI sign-in and CodexBar authorization are separate. Authorize CodexBar on the machine running Kandev, using the same operating-system user and CodexBar configuration environment. Complete GitHub's device authorization, then refresh usage."}
		failure.Setup = setup
		if cmd.Err != nil || len(cmd.Argv) == 0 {
			setup.Hint += " Resolve the CodexBar CLI connection in Shared settings → Advanced, then refresh usage."
			continue
		}
		// Help is read-only. Never start device login from the poller. Probe the
		// effective executable rather than guessing support from its version;
		// the independently versioned Windows port may differ.
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		out, err := p.run(probeCtx, cmd.Argv[0], append(argvTail(cmd), "config", "login", "--help")...)
		cancel()
		help := strings.Join(strings.Fields(strings.ToLower(string(out))), " ")
		if err == nil && strings.Contains(help, "config login --provider copilot") {
			setup.Command, setup.Shell = copilotLoginCommand(cmd, runtime.GOOS)
		}
		if setup.Command == "" {
			setup.Hint += " This CLI's device-login support could not be verified. Upgrade CodexBar to a release supporting config login --provider copilot, then refresh usage."
			if cmd.Source == sourceDownload {
				setup.Hint += " Use Shared settings → Advanced → Update CodexBar."
			} else {
				setup.Hint += " Update the configured or PATH installation externally."
			}
		}
	}
}

func copilotLoginCommand(cmd resolvedCommand, platform string) (string, string) {
	args := append(append([]string(nil), cmd.Argv...), "config", "login", "--provider", "copilot")
	if len(cmd.Argv) == 0 {
		return "", ""
	}
	for i, arg := range args {
		if strings.ContainsAny(arg, "\x00\r\n") {
			return "", ""
		}
		if platform == "windows" {
			args[i] = "'" + strings.ReplaceAll(arg, "'", "''") + "'"
		} else {
			args[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
		}
	}
	if platform == "windows" {
		return "& " + strings.Join(args, " "), "PowerShell"
	}
	return strings.Join(args, " "), "POSIX shell"
}
