package main

import (
	"archive/tar"
	"archive/zip"
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

// pinnedVersion is the upstream codexbar release the macOS and Linux
// auto-download path fetches. Pinned so the `usage --format json` shape can't
// drift under us; bump deliberately after re-probing the output and refreshing
// the per-platform checksums below.
const pinnedVersion = "0.45.2"

// winPinnedVersion is the Win-CodexBar release the Windows path fetches. It is
// versioned separately because that port cuts its own releases and skips
// upstream versions entirely — the two numbers do not track each other.
const winPinnedVersion = "0.56.8"

// codexbarBinName is the executable inside every upstream CodexBarCLI tarball;
// the Windows port names its own binary differently.
const codexbarBinName = "CodexBarCLI"
const winCodexbarBinName = "codexbar-cli.exe"

// platformAsset is one platform's CLI download: where to get it, the pinned
// SHA-256 from the release's published .sha256 sidecar, what the executable
// inside is called, and whether the archive is a zip rather than a tarball.
type platformAsset struct {
	// suffix names the artifact within the cache path, so an artifact change at
	// the same version (such as glibc to static musl) can't reuse an
	// incompatible cached binary.
	suffix  string
	sha256  string
	url     string
	binName string
	zipped  bool
}

// codexbarAssets maps GOOS-GOARCH to its CLI download. macOS and Linux come from
// upstream codexbar; Windows has no upstream build, so it comes from
// Win-CodexBar, a third-party port that publishes a compatible CLI.
var codexbarAssets = map[string]platformAsset{
	"linux-amd64":  upstreamAsset("linux-musl-x86_64", "7397da556d6400e9c069953c89e6bdbbc41b82d1ddd8b1fe6af576bef9f98487"),
	"linux-arm64":  upstreamAsset("linux-musl-aarch64", "b209659765da51aaad471ffa194d808e85ca8f59c4b68be86e9045ea5c10bae0"),
	"darwin-amd64": upstreamAsset("macos-x86_64", "fb433b69f91b1459a6be2f1c630814eb71f84e9e63ed28bce0e56a3bea6feb5a"),
	"darwin-arm64": upstreamAsset("macos-arm64", "df83f412016bbb70c3011ae2c38e36fc211c39cae7e4dc7c655b6c968622e7bc"),
	"windows-amd64": {
		suffix:  "windows-x64",
		sha256:  "9c547e004f219d4e226ef557bc1cdae790cae6534aa013a895642585dd10e2be",
		url:     winCodexbarURL("windows-x64"),
		binName: winCodexbarBinName,
		zipped:  true,
	},
}

func upstreamAsset(suffix, sha string) platformAsset {
	return platformAsset{suffix: suffix, sha256: sha, url: codexbarURL(suffix), binName: codexbarBinName}
}

// installErrKind classifies why the auto-download path couldn't produce a
// runnable codexbar. The Settings card pairs the raw error with a per-kind hint
// (see installHint), so the operator sees what to do, not just what broke.
type installErrKind string

const (
	installErrUnsupported installErrKind = "unsupported_platform" // no build for this GOOS/GOARCH
	installErrDownload    installErrKind = "download"             // fetching the release asset failed
	installErrChecksum    installErrKind = "checksum"             // asset didn't match the pinned SHA-256
	installErrUnpack      installErrKind = "unpack"               // gunzip/untar/publish into the cache failed
)

// installError carries the classification plus the context the hint needs: the
// release URL for download failures, the cache directory for unpack failures.
type installError struct {
	Kind installErrKind
	URL  string
	Path string
	// Version is the release the failed download was pinned to. Carried because
	// it differs per platform: the Windows port versions independently.
	Version string
	Err     error
}

func (e *installError) Error() string { return e.Err.Error() }
func (e *installError) Unwrap() error { return e.Err }

// codexbarURL is the upstream release download URL for a platform suffix.
func codexbarURL(suffix string) string {
	return fmt.Sprintf(
		"https://github.com/steipete/CodexBar/releases/download/v%s/CodexBarCLI-v%s-%s.tar.gz",
		pinnedVersion, pinnedVersion, suffix,
	)
}

// winCodexbarURL is the Win-CodexBar release download URL. It keeps upstream's
// CodexBarCLI-v<version>-<platform> naming, but ships a zip.
func winCodexbarURL(suffix string) string {
	return fmt.Sprintf(
		"https://github.com/nesszer/Win-CodexBar/releases/download/v%s/CodexBarCLI-v%s-%s.zip",
		winPinnedVersion, winPinnedVersion, suffix,
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

// binPath is where the pinned binary for this platform is cached. Including the
// asset suffix prevents an artifact change at the same version (such as glibc to
// static musl) from reusing an incompatible cached binary.
func (d *downloader) binPath() string {
	asset, ok := codexbarAssets[d.platform]
	if !ok {
		return filepath.Join(d.cacheDir, "codexbar", pinnedVersion, codexbarBinName)
	}
	return filepath.Join(d.cacheDir, "codexbar", asset.version(), asset.suffix, asset.binName)
}

// version is the release the asset was pinned to. The Windows port versions
// independently of upstream, so this cannot be a single constant.
func (a platformAsset) version() string {
	if a.zipped {
		return winPinnedVersion
	}
	return pinnedVersion
}

// ensure returns a path to a ready-to-run codexbar binary, downloading and
// caching the pinned per-platform build on first use. Cheap on the warm path
// (a stat of the cached binary).
func (d *downloader) ensure(ctx context.Context) (string, error) {
	asset, ok := codexbarAssets[d.platform]
	if !ok {
		return "", &installError{
			Kind: installErrUnsupported,
			Err:  fmt.Errorf("codexbar has no prebuilt CLI for %s", d.platform),
		}
	}
	bin := d.binPath()
	if isRunnableFile(bin) {
		return bin, nil
	}
	if err := d.install(ctx, asset, filepath.Dir(bin)); err != nil {
		return "", err
	}
	return bin, nil
}

// install downloads the asset archive, verifies its SHA-256, and atomically
// extracts its whole contents into versionDir. The whole archive is kept (not
// just the binary) because upstream codexbar reads its sibling VERSION file to
// report its own version; the executable is the member named asset.binName.
func (d *downloader) install(ctx context.Context, asset platformAsset, versionDir string) error {
	url, version := asset.url, asset.version()
	body, err := d.fetch(ctx, url)
	if err != nil {
		return &installError{Kind: installErrDownload, URL: url, Version: version, Err: fmt.Errorf("downloading codexbar: %w", err)}
	}
	defer body.Close()

	raw, err := io.ReadAll(body)
	if err != nil {
		return &installError{Kind: installErrDownload, URL: url, Version: version, Err: fmt.Errorf("reading codexbar download: %w", err)}
	}
	if got := sha256Hex(raw); got != asset.sha256 {
		return &installError{
			Kind:    installErrChecksum,
			URL:     url,
			Version: version,
			Err:     fmt.Errorf("codexbar checksum mismatch: expected %s, got %s", asset.sha256, got),
		}
	}
	extract := extractTarGz
	if asset.zipped {
		extract = extractZip
	}
	if err := extract(raw, versionDir, asset.binName); err != nil {
		return &installError{Kind: installErrUnpack, URL: url, Version: version, Path: versionDir, Err: err}
	}
	return nil
}

// extractZip unpacks a codexbar zip into destDir with the same contract as
// extractTarGz. Win-CodexBar ships a single flat codexbar-cli.exe, but the whole
// archive is kept for symmetry with the tarball path.
func extractZip(zipped []byte, destDir, execName string) error {
	zr, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil {
		return fmt.Errorf("opening codexbar zip: %w", err)
	}
	return publishExtraction(destDir, execName, "zip", func(tmp string) (bool, error) {
		var foundExec bool
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			base := filepath.Base(f.Name)
			if base == "." || base == ".." || base == "" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return foundExec, fmt.Errorf("opening %s: %w", base, err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return foundExec, fmt.Errorf("extracting %s: %w", base, err)
			}
			mode := os.FileMode(0o644)
			if base == execName {
				mode = 0o755
				foundExec = true
			}
			if err := os.WriteFile(filepath.Join(tmp, base), data, mode); err != nil {
				return foundExec, fmt.Errorf("writing %s: %w", base, err)
			}
		}
		return foundExec, nil
	})
}

// publishExtraction runs write into a temporary sibling of destDir and renames
// it into place, so a partial download never looks installed. Errors when write
// did not produce execName.
func publishExtraction(destDir, execName, archive string, write func(tmp string) (bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}
	tmp := destDir + ".tmp-extract"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fmt.Errorf("creating temp cache dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	foundExec, err := write(tmp)
	if err != nil {
		return err
	}
	if !foundExec {
		return fmt.Errorf("%s not found in codexbar %s", execName, archive)
	}

	_ = os.RemoveAll(destDir)
	if err := os.Rename(tmp, destDir); err != nil {
		return fmt.Errorf("publishing codexbar: %w", err)
	}
	return nil
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

	return publishExtraction(destDir, execName, "tarball", func(tmp string) (bool, error) {
		return writeTarMembers(tar.NewReader(gz), tmp, execName)
	})
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

// isRunnableFile reports whether path is a regular file this host would run.
// It asks about the running OS, not about which asset was downloaded: Windows
// carries no executable bit — Go reports mode 0666 for every file there,
// including a .exe — so being a regular file is the whole test. Checking the bit
// anyway would leave the cache permanently cold, re-downloading on every poll.
func isRunnableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
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
