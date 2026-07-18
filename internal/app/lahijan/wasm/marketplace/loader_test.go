// loader_test.go covers the YAML index parsing + cache hit/miss paths
// for the local + HTTP index loaders. The HTTP loader uses httptest so
// no real network is involved.
package marketplace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeIndex writes a YAML index file into a temp directory and returns
// the directory path. The file is named plugins-marketplace.yaml per
// the in-repo marketplace convention.
func writeIndex(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugins-marketplace.yaml"), []byte(body), 0o644))
	return dir
}

const validIndexYAML = `
version: 1
updated_at: "2026-07-18T00:00:00Z"
plugins:
  - name: slack-notifier
    version: 1.0.0
    description: "Posts alerts to Slack"
    permissions:
      - "network.outbound:hooks.slack.com"
      - "events.listen:compute.instance.*"
      - "config.read:slack-notifier"
    source:
      repo: local
      path: slack-notifier
    sha256: "abc123"
  - name: dns-record-hook
    version: 1.0.0
    description: "Webhook forwarder"
    permissions:
      - "events.listen:dns.record.*"
      - "network.outbound:example.com"
      - "config.read:dns-record-hook"
    source:
      repo: local
      path: dns-record-hook
    sha256: "def456"
`

func TestLocalIndexLoader_ParsesValidIndex(t *testing.T) {
	t.Parallel()
	dir := writeIndex(t, validIndexYAML)
	loader := NewLocalIndexLoader(dir, time.Minute)

	idx, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, idx.Version)
	require.Len(t, idx.Plugins, 2)
	assert.Equal(t, "slack-notifier", idx.Plugins[0].Name)
	assert.Equal(t, "1.0.0", idx.Plugins[0].Version)
	require.Len(t, idx.Plugins[0].Permissions, 3)
	assert.Equal(t, "network.outbound:hooks.slack.com", idx.Plugins[0].Permissions[0])
	assert.Equal(t, "local", idx.Plugins[0].Source.Repo)
	assert.Equal(t, "slack-notifier", idx.Plugins[0].Source.Path)
}

func TestLocalIndexLoader_RejectsMissingVersion(t *testing.T) {
	t.Parallel()
	dir := writeIndex(t, `
plugins:
  - name: x
    version: 1.0.0
    source: { repo: local }
`)
	_, err := NewLocalIndexLoader(dir, 0).Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing 'version'")
}

func TestLocalIndexLoader_RejectsEmptyPluginsList(t *testing.T) {
	t.Parallel()
	dir := writeIndex(t, `
version: 1
plugins: []
`)
	_, err := NewLocalIndexLoader(dir, 0).Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugins")
}

func TestLocalIndexLoader_RejectsDuplicateNames(t *testing.T) {
	t.Parallel()
	dir := writeIndex(t, `
version: 1
plugins:
  - name: dup
    version: 1.0.0
    source: { repo: local }
  - name: dup
    version: 2.0.0
    source: { repo: local }
`)
	_, err := NewLocalIndexLoader(dir, 0).Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate plugin name")
}

func TestLocalIndexLoader_RejectsMissingSourceRepo(t *testing.T) {
	t.Parallel()
	dir := writeIndex(t, `
version: 1
plugins:
  - name: bad
    version: 1.0.0
    source: {}
`)
	_, err := NewLocalIndexLoader(dir, 0).Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing source.repo")
}

func TestLocalIndexLoader_CacheHitOnRepeatedLoad(t *testing.T) {
	t.Parallel()
	dir := writeIndex(t, validIndexYAML)
	loader := NewLocalIndexLoader(dir, time.Minute)

	first, err := loader.Load(context.Background())
	require.NoError(t, err)
	second, err := loader.Load(context.Background())
	require.NoError(t, err)
	// Same pointer = cache hit (no re-parse).
	assert.Same(t, first, second)
}

func TestLocalIndexLoader_RereadsAfterMtimeChange(t *testing.T) {
	t.Parallel()
	dir := writeIndex(t, validIndexYAML)
	loader := NewLocalIndexLoader(dir, time.Minute)

	first, err := loader.Load(context.Background())
	require.NoError(t, err)

	// Bump the mtime forward past the loader's `loaded` timestamp by
	// writing a different file (new plugin count) and touching the
	// mtime so the equality check fails.
	newBody := `
version: 1
plugins:
  - name: only-one
    version: 1.0.0
    source: { repo: local }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugins-marketplace.yaml"), []byte(newBody), 0o644))
	// Set mtime to the future so stat.ModTime().Equal(cached) fails.
	future := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "plugins-marketplace.yaml"), future, future))

	second, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.NotSame(t, first, second)
	require.Len(t, second.Plugins, 1)
	assert.Equal(t, "only-one", second.Plugins[0].Name)
}

func TestHTTPIndexLoader_ParsesRemoteIndex(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(validIndexYAML))
	}))
	defer srv.Close()

	loader := NewHTTPIndexLoader(srv.URL, time.Minute)
	idx, err := loader.Load(context.Background())
	require.NoError(t, err)
	require.Len(t, idx.Plugins, 2)
	assert.Equal(t, "slack-notifier", idx.Plugins[0].Name)
}

func TestHTTPIndexLoader_PropagatesNon200(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := NewHTTPIndexLoader(srv.URL, 0).Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestHTTPIndexLoader_CacheHitOnRepeatedLoad(t *testing.T) {
	t.Parallel()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(validIndexYAML))
	}))
	defer srv.Close()

	loader := NewHTTPIndexLoader(srv.URL, time.Minute)
	_, err := loader.Load(context.Background())
	require.NoError(t, err)
	_, err = loader.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, hits, "second load should hit the cache")
}
