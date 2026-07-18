// assets_test.go covers the local asset loader + sha256 verification.
// The HTTP asset loader is exercised in the integration test.
package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writePlugin writes a minimal plugin tree (manifest + wasm) into a
// fresh subdirectory of the test temp dir. Returns the path to the
// root that should be passed to NewLocalAssetLoader.
func writePlugin(t *testing.T, name, manifest string, wasm []byte) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lahijan.manifest.yaml"), []byte(manifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.wasm"), wasm, 0o644))
	return root
}

func TestLocalAssetLoader_ReadsFiles(t *testing.T) {
	t.Parallel()
	wasm := []byte("\x00asm\x01\x00\x00\x00some-body")
	root := writePlugin(t, "test-plugin", "name: test-plugin\nversion: 1.0.0\n", wasm)
	loader := NewLocalAssetLoader(root)

	asset, err := loader.Fetch(context.Background(), Entry{
		Name: "test-plugin",
		Source: Source{Repo: "local", Path: "test-plugin"},
	})
	require.NoError(t, err)
	assert.Equal(t, wasm, asset.WasmBytes)
	assert.Equal(t, "name: test-plugin\nversion: 1.0.0\n", string(asset.ManifestYAML))
}

func TestLocalAssetLoader_FallsBackToEntryNameWhenPathEmpty(t *testing.T) {
	t.Parallel()
	root := writePlugin(t, "name-only", "name: name-only\nversion: 1.0.0\n", []byte("bytes"))
	loader := NewLocalAssetLoader(root)
	_, err := loader.Fetch(context.Background(), Entry{Name: "name-only", Source: Source{Repo: "local"}})
	require.NoError(t, err)
}

func TestLocalAssetLoader_MissingManifestFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "p"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "p", "plugin.wasm"), []byte("x"), 0o644))
	loader := NewLocalAssetLoader(root)
	_, err := loader.Fetch(context.Background(), Entry{Name: "p", Source: Source{Repo: "local", Path: "p"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read manifest")
}

func TestLocalAssetLoader_MissingWasmFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "p"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "p", "lahijan.manifest.yaml"), []byte("name: p\nversion: 1.0.0\n"), 0o644))
	loader := NewLocalAssetLoader(root)
	_, err := loader.Fetch(context.Background(), Entry{Name: "p", Source: Source{Repo: "local", Path: "p"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read wasm")
}

func TestVerifySHA256_AcceptsMatchingHash(t *testing.T) {
	t.Parallel()
	wasm := []byte("hello world")
	sum := sha256.Sum256(wasm)
	hex := hex.EncodeToString(sum[:])
	require.NoError(t, verifySHA256(wasm, hex))
}

func TestVerifySHA256_RejectsMismatch(t *testing.T) {
	t.Parallel()
	wasm := []byte("hello world")
	err := verifySHA256(wasm, "0000000000000000000000000000000000000000000000000000000000000000")
	require.ErrorIs(t, err, ErrHashMismatch)
}

func TestVerifySHA256_SkipsPlaceholder(t *testing.T) {
	t.Parallel()
	// The default in-repo marketplace ships REPLACE_ME_AFTER_BUILD so
	// operators can vendor the repo without computing hashes up-front.
	// Verification skips the check on this sentinel.
	require.NoError(t, verifySHA256([]byte("anything"), "REPLACE_ME_AFTER_BUILD"))
	require.NoError(t, verifySHA256([]byte("anything"), ""))
}

func TestHTTPAssetLoader_RejectsGitSource(t *testing.T) {
	t.Parallel()
	loader := NewHTTPAssetLoader("http://example.test")
	_, err := loader.Fetch(context.Background(), Entry{
		Name:   "p",
		Source: Source{Repo: "git", GitURL: "https://example.com/p.git"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git-sourced plugins are not yet supported")
}
