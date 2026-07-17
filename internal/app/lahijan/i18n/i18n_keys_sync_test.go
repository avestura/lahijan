// i18n_keys_sync_test.go enforces ADR-0017's "en.json and fa.json keys match"
// CI check. It flattens both locale JSON trees into a set of dotted key paths
// and fails with a diff if they diverge. Run as a normal unit test (no
// integration tag needed) so it runs on every PR.
package i18n

import (
	"embed"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed locales/*.json
var syncFS embed.FS

// flattenKeys walks a decoded JSON map and returns every leaf dotted key path,
// sorted. A "leaf" is any value that is not itself a map. This matches how
// go-i18n message ids nest under their locale tree.
func flattenKeys(prefix string, in map[string]any, out *[]string) {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := in[k]
		full := k
		if prefix != "" {
			full = prefix + "." + k
		}
		if child, ok := v.(map[string]any); ok {
			flattenKeys(full, child, out)
			continue
		}
		*out = append(*out, full)
	}
}

// loadLocaleKeys reads one locale file from the embedded FS and returns its
// sorted leaf keys.
func loadLocaleKeys(t *testing.T, name string) []string {
	t.Helper()
	data, err := syncFS.ReadFile("locales/" + name)
	require.NoError(t, err, "read %s", name)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw), "parse %s", name)
	var keys []string
	flattenKeys("", raw, &keys)
	sort.Strings(keys)
	return keys
}

func TestLocaleKeys_enAndFaInSync(t *testing.T) {
	t.Parallel()
	en := loadLocaleKeys(t, "en.json")
	fa := loadLocaleKeys(t, "fa.json")

	enSet := map[string]struct{}{}
	for _, k := range en {
		enSet[k] = struct{}{}
	}
	faSet := map[string]struct{}{}
	for _, k := range fa {
		faSet[k] = struct{}{}
	}

	var missingInFa, missingInEn []string
	for _, k := range en {
		if _, ok := faSet[k]; !ok {
			missingInFa = append(missingInFa, k)
		}
	}
	for _, k := range fa {
		if _, ok := enSet[k]; !ok {
			missingInEn = append(missingInEn, k)
		}
	}

	if len(missingInFa) != 0 || len(missingInEn) != 0 {
		t.Errorf(
			"locale keys out of sync:\n  missing in fa.json: %s\n  missing in en.json: %s",
			strings.Join(missingInFa, ", "),
			strings.Join(missingInEn, ", "),
		)
	}

	assert.Equal(t, en, fa, "en.json and fa.json must have identical key sets")
}
