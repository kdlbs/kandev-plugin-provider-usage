package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// pinnedVersion is the codexbar release the auto-download path fetches. Pinned so
// the `usage --format json` shape can't drift under us; bump deliberately after
// re-probing the output and refreshing the per-platform checksums below.
const pinnedVersion = "0.45.2"

// codexbarBinName is the executable inside every CodexBarCLI release tarball.
const codexbarBinName = "CodexBarCLI"

// platformAsset is a codexbar release asset for one Go platform: the tarball's
// platform suffix and the pinned SHA-256 of the .tar.gz (from the release's
// published .sha256 sidecars).
type platformAsset struct {
	suffix string
	sha256 string
}

// codexbarAssets maps GOOS-GOARCH to the codexbar release asset. codexbar ships
// macOS and Linux CLI builds only — Windows has no entry and degrades to
// "configure a codexbar path" guidance.
var codexbarAssets = map[string]platformAsset{
	"linux-amd64":  {"linux-x86_64", "f5ca9e5bbe511493902bd8fd7d2c409c9b4800259967284a05a73627156a5f2e"},
	"linux-arm64":  {"linux-aarch64", "d5635c9e5b7524ecd4aa91d0de30a3c18f3c9d1fcaa3920187a6d6c7f3b8bbc0"},
	"darwin-amd64": {"macos-x86_64", "fb433b69f91b1459a6be2f1c630814eb71f84e9e63ed28bce0e56a3bea6feb5a"},
	"darwin-arm64": {"macos-arm64", "df83f412016bbb70c3011ae2c38e36fc211c39cae7e4dc7c655b6c968622e7bc"},
}

// codexbarURL is the GitHub release download URL for a platform suffix.
func codexbarURL(suffix string) string {
	return fmt.Sprintf(
		"https://github.com/steipete/CodexBar/releases/download/v%s/CodexBarCLI-v%s-%s.tar.gz",
		pinnedVersion, pinnedVersion, suffix,
	)
}

// fetcher retrieves a URL's body — net/http in production, injected for tests.
type fetcher func(ctx context.Context, url string) (io.ReadCloser, error)

// downloader resolves and caches the pinned codexbar binary for the current
// platform under a per-user cache dir.
type downloader struct {
	cacheDir string // e.g. ~/.config/kandev-provider-usage
	platform string // GOOS-GOARCH
	fetch    fetcher
}

func newDownloader() *downloader {
	return &downloader{
		cacheDir: cacheRoot(),
		platform: runtime.GOOS + "-" + runtime.GOARCH,
		fetch:    httpFetch,
	}
}

// cacheRoot picks a per-user cache directory, preferring $XDG_CONFIG_HOME /
// ~/.config, falling back to the OS temp dir when no home is available.
func cacheRoot() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "kandev-provider-usage")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "kandev-provider-usage")
	}
	return filepath.Join(os.TempDir(), "kandev-provider-usage")
}

// binPath is where the pinned binary for this platform is cached.
func (d *downloader) binPath() string {
	return filepath.Join(d.cacheDir, "codexbar", pinnedVersion, codexbarBinName)
}

// ensure returns a path to a ready-to-run codexbar binary, downloading and
// caching the pinned per-platform build on first use. Cheap on the warm path
// (a stat of the cached binary).
func (d *downloader) ensure(ctx context.Context) (string, error) {
	asset, ok := codexbarAssets[d.platform]
	if !ok {
		return "", fmt.Errorf(
			"codexbar has no prebuilt CLI for %s — set a codexbar path in Settings > Plugins > Provider Usage",
			d.platform)
	}
	bin := d.binPath()
	if isExecutableFile(bin) {
		return bin, nil
	}
	if err := d.install(ctx, asset, filepath.Dir(bin)); err != nil {
		return "", err
	}
	return bin, nil
}

// install downloads the asset tarball, verifies its SHA-256, and atomically
// extracts its whole contents into versionDir. The full tarball is kept (not
// just the binary) because codexbar reads its sibling VERSION file to report its
// own version; the executable is the member named codexbarBinName.
func (d *downloader) install(ctx context.Context, asset platformAsset, versionDir string) error {
	body, err := d.fetch(ctx, codexbarURL(asset.suffix))
	if err != nil {
		return fmt.Errorf("downloading codexbar: %w", err)
	}
	defer body.Close()

	raw, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("reading codexbar download: %w", err)
	}
	if got := sha256Hex(raw); got != asset.sha256 {
		return fmt.Errorf("codexbar checksum mismatch: expected %s, got %s", asset.sha256, got)
	}
	return extractTarGz(raw, versionDir, codexbarBinName)
}

// extractTarGz gunzips + untars a codexbar tarball into destDir, flattening
// members by base name and marking execName executable. It extracts into a
// temporary sibling dir and renames it into place so a partial download never
// looks installed. Errors when execName isn't present.
func extractTarGz(tarGz []byte, destDir, execName string) error {
	gz, err := gzip.NewReader(bytes.NewReader(tarGz))
	if err != nil {
		return fmt.Errorf("opening codexbar gzip: %w", err)
	}
	defer gz.Close()

	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}
	tmp := destDir + ".tmp-extract"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fmt.Errorf("creating temp cache dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	foundExec, err := writeTarMembers(tar.NewReader(gz), tmp, execName)
	if err != nil {
		return err
	}
	if !foundExec {
		return fmt.Errorf("%s not found in codexbar tarball", execName)
	}

	_ = os.RemoveAll(destDir)
	if err := os.Rename(tmp, destDir); err != nil {
		return fmt.Errorf("publishing codexbar: %w", err)
	}
	return nil
}

// writeTarMembers writes each regular file in tr into destDir (flattened by base
// name), reporting whether execName was among them.
func writeTarMembers(tr *tar.Reader, destDir, execName string) (bool, error) {
	var foundExec bool
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return foundExec, nil
		}
		if err != nil {
			return foundExec, fmt.Errorf("reading codexbar tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(hdr.Name)
		if base == "." || base == ".." || base == "" {
			continue
		}
		mode := os.FileMode(0o644)
		if base == execName {
			mode = 0o755
			foundExec = true
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return foundExec, fmt.Errorf("extracting %s: %w", base, err)
		}
		if err := os.WriteFile(filepath.Join(destDir, base), data, mode); err != nil {
			return foundExec, fmt.Errorf("writing %s: %w", base, err)
		}
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// isExecutableFile reports whether path is a regular file with an executable bit.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

// httpFetch is the production fetcher: a context-bound GET that returns the
// response body, following GitHub's redirect to release storage.
func httpFetch(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}
	return resp.Body, nil
}
