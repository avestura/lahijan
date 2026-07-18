package marketplace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// IndexLoader reads the plugins-marketplace.yaml index from a source
// (local directory or remote URL). Implementations MUST be safe for
// concurrent use; the service caches the index for a configurable TTL
// and re-reads on cache expiry.
type IndexLoader interface {
	// Load returns the parsed index. Implementations should cache
	// aggressively; the marketplace list endpoint is hit on every page
	// load.
	Load(ctx context.Context) (*Index, error)
}

// LocalIndexLoader reads plugins-marketplace.yaml from a directory on
// the local filesystem. Used for the in-repo marketplace and for
// operators who vendor a private marketplace directory.
type LocalIndexLoader struct {
	dir     string
	ttl     time.Duration
	cached  *Index
	loaded  time.Time
	modTime time.Time
}

// NewLocalIndexLoader builds a loader rooted at dir. The directory must
// contain plugins-marketplace.yaml at its top level. ttl is the cache
// lifetime; a value of 0 disables caching (re-read on every call).
func NewLocalIndexLoader(dir string, ttl time.Duration) *LocalIndexLoader {
	return &LocalIndexLoader{dir: dir, ttl: ttl}
}

// Load implements IndexLoader.
func (l *LocalIndexLoader) Load(ctx context.Context) (*Index, error) {
	path := filepath.Join(l.dir, "plugins-marketplace.yaml")
	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("marketplace: stat index %s: %w", path, err)
	}
	// Cache hit when within TTL AND the file's mtime is unchanged.
	if l.cached != nil && (l.ttl == 0 || time.Since(l.loaded) < l.ttl) && stat.ModTime().Equal(l.modTime) {
		return l.cached, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("marketplace: read index %s: %w", path, err)
	}
	idx, err := parseIndex(raw)
	if err != nil {
		return nil, fmt.Errorf("marketplace: parse index %s: %w", path, err)
	}
	l.cached = idx
	l.loaded = time.Now()
	l.modTime = stat.ModTime()
	return idx, nil
}

// HTTPIndexLoader fetches the index from a remote URL. Used when
// conf.wasm.marketplace.url is set to an HTTP(S) endpoint. The remote
// server must serve plugins-marketplace.yaml verbatim.
type HTTPIndexLoader struct {
	url    string
	ttl    time.Duration
	hc     *http.Client
	cached *Index
	loaded time.Time
}

// NewHTTPIndexLoader builds a remote loader. The url must point at a
// server that serves plugins-marketplace.yaml as the response body (no
// wrapping envelope). ttl is the cache lifetime.
func NewHTTPIndexLoader(url string, ttl time.Duration) *HTTPIndexLoader {
	return &HTTPIndexLoader{
		url: strings.TrimSpace(url),
		ttl: ttl,
		hc:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Load implements IndexLoader.
func (l *HTTPIndexLoader) Load(ctx context.Context) (*Index, error) {
	if l.cached != nil && l.ttl > 0 && time.Since(l.loaded) < l.ttl {
		return l.cached, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.url, nil)
	if err != nil {
		return nil, fmt.Errorf("marketplace: build request %s: %w", l.url, err)
	}
	resp, err := l.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("marketplace: fetch %s: %w", l.url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("marketplace: %s returned %s", l.url, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return nil, fmt.Errorf("marketplace: read %s: %w", l.url, err)
	}
	idx, err := parseIndex(raw)
	if err != nil {
		return nil, fmt.Errorf("marketplace: parse %s: %w", l.url, err)
	}
	l.cached = idx
	l.loaded = time.Now()
	return idx, nil
}

// parseIndex is the single point that turns YAML bytes into an Index.
// Pulled out so local + HTTP loaders share validation.
func parseIndex(raw []byte) (*Index, error) {
	var idx Index
	if err := yaml.Unmarshal(raw, &idx); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if idx.Version == 0 {
		return nil, errors.New("marketplace: index missing 'version'")
	}
	if len(idx.Plugins) == 0 {
		return nil, errors.New("marketplace: index has no plugins")
	}
	// Validate per-entry invariants. The installer re-validates the
	// manifest at install time, so this is best-effort + admin-facing.
	names := make(map[string]struct{}, len(idx.Plugins))
	for i, e := range idx.Plugins {
		if e.Name == "" {
			return nil, fmt.Errorf("marketplace: plugins[%d] missing name", i)
		}
		if e.Version == "" {
			return nil, fmt.Errorf("marketplace: plugins[%d] (%s) missing version", i, e.Name)
		}
		if e.Source.Repo == "" {
			return nil, fmt.Errorf("marketplace: plugins[%d] (%s) missing source.repo", i, e.Name)
		}
		if _, dup := names[e.Name]; dup {
			return nil, fmt.Errorf("marketplace: duplicate plugin name %q", e.Name)
		}
		names[e.Name] = struct{}{}
	}
	return &idx, nil
}
