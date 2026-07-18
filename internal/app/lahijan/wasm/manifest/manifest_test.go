// manifest_test.go covers the manifest parser + validator without touching
// the database. Manifests are the install-time source of truth, so a typo
// here is a security issue (an admin might approve a permission that does
// not exist).

package manifest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_HappyPath(t *testing.T) {
	t.Parallel()
	raw := []byte(`
name: slack-notifier
version: 1.2.0
description: "Posts alerts to Slack"
author: "Example <ops@example.com>"
license: Apache-2.0
permissions:
  - network.outbound
  - kv.read:cache
  - events.listen:dns.record.*
entrypoints:
  - on_event
`)
	m, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "slack-notifier", m.Name)
	assert.Equal(t, "1.2.0", m.Version)
	assert.Equal(t, []string{"network.outbound", "kv.read:cache", "events.listen:dns.record.*"}, m.Permissions)
	assert.Equal(t, []string{"on_event"}, m.Entrypoints)
}

func TestParse_RejectsBadName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"empty":         "name: ''\nversion: 1.0.0\n",
		"uppercase":     "name: SlackNotifier\nversion: 1.0.0\n",
		"underscore":    "name: slack_notifier\nversion: 1.0.0\n",
		"leading dash":  "name: -slack\nversion: 1.0.0\n",
		"trailing dash": "name: slack-\nversion: 1.0.0\n",
		"double dash":   "name: slack--notifier\nversion: 1.0.0\n",
		"too long":      "name: " + repeat("a", 65) + "\nversion: 1.0.0\n",
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "manifest: name")
		})
	}
}

func TestParse_RejectsBadVersion(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"missing patch": "name: foo\nversion: 1.2\n",
		"non-numeric":   "name: foo\nversion: 1.2.x\n",
		"empty":         "name: foo\nversion: \"\"\n",
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "manifest: version")
		})
	}
}

func TestParse_AcceptsSemverSuffixes(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"1.0.0", "1.0.0-rc1", "1.0.0+build.5", "1.2.3-beta.1+exp.sha.5114f85"} {
		yaml := "name: foo\nversion: " + v + "\n"
		_, err := Parse([]byte(yaml))
		require.NoErrorf(t, err, "expected %s to validate", v)
	}
}

func TestParse_RejectsUnknownPermission(t *testing.T) {
	t.Parallel()
	raw := []byte(`
name: foo
version: 1.0.0
permissions:
  - network.outbound
  - totally.bogus
`)
	_, err := Parse(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "totally.bogus")
}

func TestParse_RejectsMalformedPermission(t *testing.T) {
	t.Parallel()
	raw := []byte(`
name: foo
version: 1.0.0
permissions:
  - "kv.read: "   # whitespace qualifier
`)
	_, err := Parse(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kv.read")
}

func TestParse_EmptyPermissionsAllowed(t *testing.T) {
	t.Parallel()
	raw := []byte("name: foo\nversion: 1.0.0\n")
	m, err := Parse(raw)
	require.NoError(t, err)
	assert.Empty(t, m.Permissions)
}

func TestParse_AggregatesErrors(t *testing.T) {
	t.Parallel()
	raw := []byte(`
name: BAD NAME
version: not-semver
permissions:
  - bogus.thing
`)
	_, err := Parse(raw)
	require.Error(t, err)
	// All three errors should appear in the joined message.
	msg := err.Error()
	assert.Contains(t, msg, "manifest: name")
	assert.Contains(t, msg, "manifest: version")
	assert.Contains(t, msg, "manifest: permissions")
	assert.Contains(t, msg, "multiple errors")
}

func TestManifest_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	raw := []byte(`
name: foo
version: 1.0.0
description: "test"
permissions:
  - kv.read:cache
entrypoints:
  - on_event
`)
	m, err := Parse(raw)
	require.NoError(t, err)

	js, err := m.MarshalJSON()
	require.NoError(t, err)

	back, err := ParseJSON(js)
	require.NoError(t, err)
	assert.Equal(t, m.Name, back.Name)
	assert.Equal(t, m.Version, back.Version)
	assert.Equal(t, m.Permissions, back.Permissions)
	assert.Equal(t, m.Entrypoints, back.Entrypoints)
}

func TestParseJSON_EmptyBytes(t *testing.T) {
	t.Parallel()
	m, err := ParseJSON(nil)
	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.Empty(t, m.Name)
}

func TestManifest_MarshalJSON_StableShape(t *testing.T) {
	t.Parallel()
	m := &Manifest{
		Name:        "foo",
		Version:     "1.0.0",
		Description: "test",
		Permissions: []string{"kv.read:cache"},
	}
	js, err := json.Marshal(m)
	require.NoError(t, err)
	// spot-check: keys exist.
	assert.Contains(t, string(js), `"name":"foo"`)
	assert.Contains(t, string(js), `"permissions":["kv.read:cache"]`)
}

// repeat returns n copies of s. Tiny local helper so the test file doesn't
// import strings just for one call.
func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
