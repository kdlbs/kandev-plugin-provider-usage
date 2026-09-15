package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

var errUpdateBusy = errors.New("A provider refresh or CodexBar download is already running. Retry shortly.")

var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// latestAsset accepts only a stable release and the exact CLI asset for this
// platform. GitHub's asset digest verifies the archive before extraction.
func (d *downloader) latestAsset(ctx context.Context) (platformAsset, error) {
	asset, ok := codexbarAssets[d.platform]
	if !ok {
		return asset, fmt.Errorf("codexbar has no prebuilt CLI for %s", d.platform)
	}
	repo, ext := "steipete/CodexBar", ".tar.gz"
	if asset.zipped {
		repo, ext = "nesszer/Win-CodexBar", ".zip"
	}
	body, err := d.fetch(ctx, "https://api.github.com/repos/"+repo+"/releases/latest")
	if err != nil {
		return asset, fmt.Errorf("checking CodexBar release: %w", err)
	}
	defer body.Close()
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 2<<20)).Decode(&release); err != nil {
		return asset, fmt.Errorf("reading CodexBar release: %w", err)
	}
	version := strings.TrimPrefix(release.Tag, "v")
	if release.Draft || release.Prerelease || !releaseVersion.MatchString(version) {
		return asset, fmt.Errorf("invalid stable CodexBar release %q", release.Tag)
	}
	name := "CodexBarCLI-v" + version + "-" + asset.suffix + ext
	expectedURL := "https://github.com/" + repo + "/releases/download/" + release.Tag + "/" + name
	for _, a := range release.Assets {
		if a.Name != name {
			continue
		}
		digest := strings.TrimPrefix(a.Digest, "sha256:")
		decoded, err := hex.DecodeString(digest)
		if !strings.HasPrefix(a.Digest, "sha256:") || err != nil || len(decoded) != 32 {
			return asset, fmt.Errorf("release asset %s has no valid SHA-256 digest", name)
		}
		if a.URL != expectedURL {
			return asset, fmt.Errorf("unexpected CodexBar asset URL")
		}
		asset.version, asset.url, asset.sha256 = version, a.URL, strings.ToLower(digest)
		return asset, nil
	}
	return asset, fmt.Errorf("CodexBar %s has no CLI asset for %s", version, d.platform)
}

func (d *downloader) selectionPath() string {
	return filepath.Join(d.cacheDir, "codexbar", "selected-"+d.platform)
}

// selectedBin resolves a persisted version only inside this platform's cache.
// A removed binary falls back to the bundled pin on the next normal refresh.
func (d *downloader) selectedBin() string {
	raw, err := os.ReadFile(d.selectionPath())
	version := strings.TrimSpace(string(raw))
	asset, ok := codexbarAssets[d.platform]
	if err != nil || !ok || !releaseVersion.MatchString(version) {
		return ""
	}
	bin := filepath.Join(d.cacheDir, "codexbar", version, asset.suffix, asset.binName)
	if !isRunnableFileForPlatform(bin, d.platform) {
		return ""
	}
	return bin
}

// update installs alongside the active binary, probes it, then atomically
// publishes the selection. Every failure leaves the previous selection intact.
func (d *downloader) update(ctx context.Context, run runner) (InstallStatus, error) {
	if !d.mu.TryLock() {
		return InstallStatus{}, errUpdateBusy
	}
	defer d.mu.Unlock()
	asset, err := d.latestAsset(ctx)
	if err != nil {
		return InstallStatus{}, err
	}
	bin := filepath.Join(d.cacheDir, "codexbar", asset.version, asset.suffix, asset.binName)
	if !isRunnableFileForPlatform(bin, d.platform) {
		if err := d.install(ctx, asset, filepath.Dir(bin)); err != nil {
			return InstallStatus{}, err
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	status := probeInstall(probeCtx, resolvedCommand{Argv: []string{bin}, Source: sourceDownload, CacheDir: d.cacheDir}, run)
	if !status.Installed {
		return status, fmt.Errorf("updated CodexBar failed its version check: %s", status.Error)
	}
	if status.Version != asset.version {
		return status, fmt.Errorf("updated CodexBar reported version %q; expected %s", status.Version, asset.version)
	}
	if err := ctx.Err(); err != nil {
		return status, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.selectionPath()), ".selected-*")
	if err != nil {
		return status, err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.WriteString(asset.version + "\n")
	closeErr := tmp.Close()
	if writeErr != nil {
		return status, writeErr
	}
	if closeErr != nil {
		return status, closeErr
	}
	if err := os.Rename(tmp.Name(), d.selectionPath()); err != nil {
		return status, err
	}
	return status, nil
}

func (p *plugin) updateWebhook(ctx context.Context, method string) *pluginsdk.WebhookResponse {
	if method != "POST" {
		return jsonResponse(405, []byte(`{"error":"use POST to update CodexBar"}`))
	}
	updateCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// Check provenance without triggering an initial install.
	if p.configuredCommand(updateCtx) != "" {
		return jsonResponse(409, []byte(`{"error":"Update your configured CLI command externally, or clear it to use managed downloads."}`))
	}
	if _, err := p.lookPath("codexbar"); err == nil {
		return jsonResponse(409, []byte(`{"error":"Update the CodexBar installation on PATH with its package manager."}`))
	}
	// Do not queue updates behind a potentially slow refresh or download.
	if !p.pollMu.TryLock() {
		return jsonResponse(409, marshalOr(map[string]string{"error": errUpdateBusy.Error()}, `{}`))
	}
	defer p.pollMu.Unlock()
	status, err := p.dl.update(updateCtx, p.run)
	if err != nil {
		if errors.Is(err, errUpdateBusy) {
			return jsonResponse(409, marshalOr(map[string]string{"error": err.Error()}, `{}`))
		}
		return jsonResponse(502, marshalOr(map[string]string{"error": err.Error()}, `{}`))
	}
	// Expire the snapshot so the next read rebuilds with the new CLI.
	p.mu.Lock()
	p.snapshot = nil
	p.mu.Unlock()
	return jsonResponse(200, marshalOr(map[string]any{"codexbar": status, "message": "CodexBar is up to date (" + status.Version + ")."}, `{}`))
}
