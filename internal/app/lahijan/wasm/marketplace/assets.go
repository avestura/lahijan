package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Asset is the bundle of bytes the installer needs to persist a plugin.
// Manifest is the raw YAML (unparsed); the installer parses + validates
// it via manifest.Parse. WasmBytes is the raw .wasm bytes; the installer
// hashes + persists + compiles them.
type Asset struct {
	ManifestYAML []byte
	WasmBytes    []byte
}

// AssetLoader fetches the .wasm + manifest for a given Entry. The
// marketplace Service delegates to one of these per install.
type AssetLoader interface {
	Fetch(ctx context.Context, e Entry) (Asset, error)
}

// LocalAssetLoader fetches plugin.wasm + lahijan.manifest.yaml from a
// directory on the local filesystem. Used for the in-repo marketplace
// (Source.Repo == "local", Source.Path == "<subdir>").
type LocalAssetLoader struct {
	rootDir string
}

// NewLocalAssetLoader builds a loader rooted at rootDir. Each Entry's
// Source.Path is interpreted relative to rootDir.
func NewLocalAssetLoader(rootDir string) *LocalAssetLoader {
	return &LocalAssetLoader{rootDir: rootDir}
}

// Fetch implements AssetLoader.
func (l *LocalAssetLoader) Fetch(ctx context.Context, e Entry) (Asset, error) {
	_ = ctx // no I/O context needed for local reads; the OS handles cancellation on process exit
	sub := e.Source.Path
	if sub == "" {
		sub = e.Name
	}
	dir := filepath.Join(l.rootDir, sub)
	manifest, err := os.ReadFile(filepath.Join(dir, "lahijan.manifest.yaml"))
	if err != nil {
		return Asset{}, fmt.Errorf("marketplace: read manifest %s: %w", dir, err)
	}
	wasm, err := os.ReadFile(filepath.Join(dir, "plugin.wasm"))
	if err != nil {
		return Asset{}, fmt.Errorf("marketplace: read wasm %s: %w", dir, err)
	}
	if err := verifySHA256(wasm, e.SHA256); err != nil {
		return Asset{}, fmt.Errorf("marketplace: %s: %w", e.Name, err)
	}
	return Asset{ManifestYAML: manifest, WasmBytes: wasm}, nil
}

// HTTPAssetLoader fetches plugin.wasm + lahijan.manifest.yaml from a
// remote base URL. Used when the marketplace index is served over HTTP
// (Source.Repo == "local" + a marketplace URL); the URL pattern is
// `<base>/<source.path>/{plugin.wasm,lahijan.manifest.yaml}`.
//
// Git-sourced plugins (Source.Repo == "git") are NOT supported by this
// loader; a future WS adds signature verification + git fetch.
type HTTPAssetLoader struct {
	baseURL string
	hc      *http.Client
}

// NewHTTPAssetLoader builds a remote asset loader. baseURL is the
// marketplace's content root (no trailing slash); the loader appends
// "<entry.source.path>/" to it.
func NewHTTPAssetLoader(baseURL string) *HTTPAssetLoader {
	return &HTTPAssetLoader{
		baseURL: baseURL,
		hc:      &http.Client{Timeout: 30 * time.Second},
	}
}

// Fetch implements AssetLoader.
func (l *HTTPAssetLoader) Fetch(ctx context.Context, e Entry) (Asset, error) {
	if e.Source.Repo == "git" {
		return Asset{}, fmt.Errorf("marketplace: %s: git-sourced plugins are not yet supported (future WS)", e.Name)
	}
	sub := e.Source.Path
	if sub == "" {
		sub = e.Name
	}
	manifest, err := l.fetchURL(ctx, l.baseURL+"/"+sub+"/lahijan.manifest.yaml")
	if err != nil {
		return Asset{}, err
	}
	wasm, err := l.fetchURL(ctx, l.baseURL+"/"+sub+"/plugin.wasm")
	if err != nil {
		return Asset{}, err
	}
	if err := verifySHA256(wasm, e.SHA256); err != nil {
		return Asset{}, fmt.Errorf("marketplace: %s: %w", e.Name, err)
	}
	return Asset{ManifestYAML: manifest, WasmBytes: wasm}, nil
}

func (l *HTTPAssetLoader) fetchURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request %s: %w", url, err)
	}
	resp, err := l.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50 MiB cap
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return body, nil
}

// ErrHashMismatch is returned by verifySHA256 when the downloaded bytes
// do not match the index's pinned hash. The installer surfaces this as
// a 422 so the admin sees the index file the marketplace has been
// tampered with.
var ErrHashMismatch = errors.New("sha256 mismatch (marketplace index is stale or compromised)")

// verifySHA256 rejects a download whose sha256 does not match the index
// pin. An empty expectedHash skips the check (used by local dev indexes
// that haven't been published yet).
func verifySHA256(actual []byte, expectedHex string) error {
	if expectedHex == "" || strings.EqualFold(expectedHex, "REPLACE_ME_AFTER_BUILD") {
		return nil
	}
	sum := sha256.Sum256(actual)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, expectedHex) {
		return fmt.Errorf("expected %s, got %s: %w", expectedHex, got, ErrHashMismatch)
	}
	return nil
}
