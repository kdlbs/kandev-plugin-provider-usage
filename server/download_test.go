package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeTarGz builds a gzip'd tar containing the given name->content members.
func makeTarGz(t *testing.T, members map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range members {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func TestExtractTarGz(t *testing.T) {
	tarGz := makeTarGz(t, map[string]string{
		"VERSION":     "0.45.2\n",
		"CodexBarCLI": "#!/bin/sh\necho hi\n",
	})
	dest := filepath.Join(t.TempDir(), "0.45.2")
	require.NoError(t, extractTarGz(tarGz, dest, "CodexBarCLI"))

	bin := filepath.Join(dest, "CodexBarCLI")
	require.True(t, isExecutableFile(bin))
	got, err := os.ReadFile(bin)
	require.NoError(t, err)
	require.Equal(t, "#!/bin/sh\necho hi\n", string(got))

	// The sibling VERSION file is preserved so codexbar can self-report.
	ver, err := os.ReadFile(filepath.Join(dest, "VERSION"))
	require.NoError(t, err)
	require.Equal(t, "0.45.2\n", string(ver))
}

func TestExtractTarGz_Missing(t *testing.T) {
	tarGz := makeTarGz(t, map[string]string{"VERSION": "x"})
	dest := filepath.Join(t.TempDir(), "0.45.2")
	err := extractTarGz(tarGz, dest, "CodexBarCLI")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
	require.NoDirExists(t, dest, "partial extraction is not published")
}

func TestDownloaderEnsure_DownloadsVerifiesCaches(t *testing.T) {
	content := "#!/bin/sh\necho codexbar\n"
	tarGz := makeTarGz(t, map[string]string{codexbarBinName: content})

	calls := 0
	d := &downloader{
		cacheDir: t.TempDir(),
		platform: "linux-amd64",
		fetch: func(_ context.Context, _ string) (io.ReadCloser, error) {
			calls++
			return io.NopCloser(bytes.NewReader(tarGz)), nil
		},
	}
	// Point the pinned checksum at our synthetic tarball for this platform.
	orig := codexbarAssets["linux-amd64"]
	codexbarAssets["linux-amd64"] = platformAsset{suffix: orig.suffix, sha256: sha256Hex(tarGz)}
	t.Cleanup(func() { codexbarAssets["linux-amd64"] = orig })

	bin, err := d.ensure(context.Background())
	require.NoError(t, err)
	require.Equal(t, filepath.Join(d.cacheDir, "codexbar", pinnedVersion, codexbarBinName), bin)
	require.True(t, isExecutableFile(bin))
	require.Equal(t, 1, calls)

	// Second call is served from cache — no re-download.
	bin2, err := d.ensure(context.Background())
	require.NoError(t, err)
	require.Equal(t, bin, bin2)
	require.Equal(t, 1, calls, "cached binary is reused")
}

func TestDownloaderEnsure_ChecksumMismatch(t *testing.T) {
	tarGz := makeTarGz(t, map[string]string{codexbarBinName: "payload"})
	d := &downloader{
		cacheDir: t.TempDir(),
		platform: "linux-amd64",
		fetch: func(_ context.Context, _ string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(tarGz)), nil
		},
	}
	// Real pinned checksum won't match the synthetic tarball -> rejected.
	_, err := d.ensure(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "checksum mismatch")
}

func TestDownloaderEnsure_UnsupportedPlatform(t *testing.T) {
	d := &downloader{cacheDir: t.TempDir(), platform: "windows-amd64", fetch: nil}
	_, err := d.ensure(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no prebuilt")
}

func TestDownloaderEnsure_FetchError(t *testing.T) {
	d := &downloader{
		cacheDir: t.TempDir(),
		platform: "linux-amd64",
		fetch: func(context.Context, string) (io.ReadCloser, error) {
			return nil, errors.New("network down")
		},
	}
	_, err := d.ensure(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "network down")
}
