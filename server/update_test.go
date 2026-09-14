package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/stretchr/testify/require"
)

func TestUpdateCodexbar(t *testing.T) {
	for _, platform := range []string{"darwin-arm64", "linux-amd64", "windows-amd64"} {
		t.Run(platform, func(t *testing.T) {
			for _, failure := range []string{"", "network", "checksum", "probe", "missing asset", "invalid version", "missing digest", "untrusted URL", "prerelease", "wrong version", "save selection"} {
				t.Run(failure, func(t *testing.T) {
					p := newTestPlugin(t, nil, nil, func(_ context.Context, _ string, _ ...string) ([]byte, error) {
						if failure == "probe" {
							return nil, errors.New("cannot execute")
						}
						if failure == "wrong version" {
							return []byte("CodexBar 8.0.0"), nil
						}
						return []byte("CodexBar 9.1.0"), nil
					})
					p.dl.platform = platform
					old := p.dl.binPath()
					require.NoError(t, os.MkdirAll(filepath.Dir(old), 0755))
					require.NoError(t, os.WriteFile(old, []byte("old binary"), 0755))
					p.snapshot = &AllProvidersReport{Codexbar: InstallStatus{Installed: true, Source: sourceDownload, Command: old}}
					asset := codexbarAssets[platform]
					raw := makeTarGz(t, map[string]string{asset.binName: "new binary", "VERSION": "9.1.0"})
					ext := ".tar.gz"
					if asset.zipped {
						raw = makeZip(t, map[string]string{asset.binName: "new binary"})
						ext = ".zip"
					}
					name := "CodexBarCLI-v9.1.0-" + asset.suffix + ext
					repo := "steipete/CodexBar"
					if asset.zipped {
						repo = "nesszer/Win-CodexBar"
					}
					assetURL := "https://github.com/" + repo + "/releases/download/v9.1.0/" + name
					digest := sha256Hex(raw)
					if failure == "checksum" {
						digest = strings.Repeat("0", 64)
					}
					if failure == "missing asset" {
						name = "unrelated.zip"
					}
					version := "v9.1.0"
					if failure == "invalid version" {
						version = "../../outside"
					}
					if failure == "missing digest" {
						digest = ""
					}
					if failure == "untrusted URL" {
						assetURL = "https://untrusted.example/cli.tar.gz"
					}
					if failure == "save selection" {
						require.NoError(t, os.MkdirAll(filepath.Join(p.dl.cacheDir, "codexbar", "selected-"+platform), 0755))
					}
					calls := 0
					p.dl.fetch = func(_ context.Context, url string) (io.ReadCloser, error) {
						calls++
						if failure == "network" {
							return nil, errors.New("offline")
						}
						if url == "https://api.github.com/repos/"+repo+"/releases/latest" {
							return io.NopCloser(strings.NewReader(fmt.Sprintf(`{"prerelease":%t,"tag_name":%q,"assets":[{"name":%q,"browser_download_url":%q,"digest":"sha256:%s"}]}`, failure == "prerelease", version, name, assetURL, digest))), nil
						}
						require.Equal(t, assetURL, url)
						return io.NopCloser(bytes.NewReader(raw)), nil
					}
					resp, err := p.HandleWebhook(context.Background(), &pluginsdk.WebhookRequest{WebhookKey: "update", Method: "POST"})
					require.NoError(t, err)
					if failure != "" {
						require.EqualValues(t, 502, resp.Status, string(resp.Body))
						got, err := p.dl.ensure(context.Background())
						require.NoError(t, err)
						require.Equal(t, old, got)
					} else {
						require.EqualValues(t, 200, resp.Status, string(resp.Body))
						var result struct {
							Codexbar InstallStatus `json:"codexbar"`
						}
						require.NoError(t, json.Unmarshal(resp.Body, &result))
						require.Equal(t, "9.1.0", result.Codexbar.Version)
						require.NotEqual(t, old, result.Codexbar.Command)
						// A fresh downloader must remember the selected version without network.
						restart := &downloader{cacheDir: p.dl.cacheDir, platform: platform, fetch: func(context.Context, string) (io.ReadCloser, error) {
							t.Fatal("unexpected fetch after restart")
							return nil, nil
						}}
						got, err := restart.ensure(context.Background())
						require.NoError(t, err)
						require.Equal(t, result.Codexbar.Command, got)
						count := calls
						resp, err = p.HandleWebhook(context.Background(), &pluginsdk.WebhookRequest{WebhookKey: "update", Method: "POST"})
						require.NoError(t, err)
						require.EqualValues(t, 200, resp.Status)
						require.Equal(t, count+1, calls, "already current should only fetch metadata")
						require.Nil(t, p.snapshot, "a successful update invalidates cached usage")
						p.dl.fetch = func(context.Context, string) (io.ReadCloser, error) { return nil, errors.New("offline") }
						resp, err = p.HandleWebhook(context.Background(), &pluginsdk.WebhookRequest{WebhookKey: "update", Method: "POST"})
						require.NoError(t, err)
						require.EqualValues(t, 502, resp.Status)
						got, err = restart.ensure(context.Background())
						require.NoError(t, err)
						require.Equal(t, result.Codexbar.Command, got, "failed subsequent updates preserve the selected version")
					}
					got, err := os.ReadFile(old)
					require.NoError(t, err)
					require.Equal(t, "old binary", string(got))
				})
			}
		})
	}
}

func TestUpdateRejectsExternalAndGet(t *testing.T) {
	for _, source := range []string{"settings", "path", "get"} {
		t.Run(source, func(t *testing.T) {
			p := newTestPlugin(t, nil, nil, nil)
			method := "POST"
			if source == "settings" {
				p.SetHost(&fakeHost{config: map[string]any{configKeyCommand: "my-codexbar"}})
			}
			if source == "path" {
				p.lookPath = func(string) (string, error) { return "/bin/codexbar", nil }
			}
			if source == "get" {
				method = "GET"
			}
			p.dl.fetch = func(context.Context, string) (io.ReadCloser, error) { t.Fatal("must not download"); return nil, nil }
			resp, err := p.HandleWebhook(context.Background(), &pluginsdk.WebhookRequest{WebhookKey: "update", Method: method})
			require.NoError(t, err)
			expected := 409
			if source == "get" {
				expected = 405
			}
			require.EqualValues(t, expected, resp.Status)
		})
	}
}
