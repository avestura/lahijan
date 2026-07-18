// installer_upgrade_test.go covers the WS-10c installer.Upgrade flow
// without touching the DB: the pure-Go semver helpers + the
// manifest-permission diff. The end-to-end upgrade path is exercised in
// installer_upgrade_integration_test.go.

package installer

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
)

func TestCompareSemver(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.0.9", "1.1.0", -1},
		{"1.99.99", "2.0.0", -1},
		// Pre-release + build suffixes are stripped before the numeric
		// compare (per the manifest's accepted subset of semver).
		{"1.0.0-rc1", "1.0.0", 0},
		{"1.0.0+build42", "1.0.0", 0},
		{"1.0.0-rc1+build42", "1.0.0", 0},
		// Missing segments default to 0 so the compare never panics.
		{"1.0", "1.0.0", 0},
		{"1", "1.0.0", 0},
		{"", "0.0.0", 0},
	}
	for _, tc := range cases {
		assert.Equalf(t, tc.want, compareSemver(tc.a, tc.b),
			"compareSemver(%q, %q)", tc.a, tc.b)
	}
}

func TestSplitSemver(t *testing.T) {
	t.Parallel()
	assert.Equal(t, [3]int{1, 2, 3}, splitSemver("1.2.3"))
	assert.Equal(t, [3]int{0, 0, 0}, splitSemver(""))
	assert.Equal(t, [3]int{42, 0, 0}, splitSemver("42"))
	// Non-numeric segment yields 0 for THAT segment, the rest still parse.
	assert.Equal(t, [3]int{1, 0, 2}, splitSemver("1.abc.2"))
	// Pre-release + build suffix on the PATCH segment are stripped.
	assert.Equal(t, [3]int{1, 2, 3}, splitSemver("1.2.3-rc1+build42"))
}

func TestManifestPermissionSet_Deduplicates(t *testing.T) {
	t.Parallel()
	m := &manifest.Manifest{
		Permissions: []string{
			"kv.read:cache",
			"kv.read:cache",
			"events.emit",
		},
	}
	set := manifestPermissionSet(m)
	assert.Len(t, set, 2)
	_, hasKV := set["kv.read:cache"]
	assert.True(t, hasKV)
	_, hasEmit := set["events.emit"]
	assert.True(t, hasEmit)
}
