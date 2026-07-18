// permission_test.go covers the prefix-match algorithm and the catalog
// invariants without touching the database.

package permission

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_AcceptsKnownSlugs(t *testing.T) {
	t.Parallel()
	for _, slug := range allCapabilities {
		require.NoErrorf(t, Validate(slug), "expected %q to validate", slug)
	}
}

func TestValidate_AcceptsQualifiers(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		"kv.read:cache",
		"kv.write:state",
		"events.listen:dns.record.created",
		"events.listen:dns.record.*",
		"kv.read:*",
		"api.handler.register:/foo",
		"config.read:my_plugin",
	} {
		require.NoErrorf(t, Validate(tc), "expected %q to validate", tc)
	}
}

func TestValidate_RejectsUnknownAndMalformed(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		"",               // empty
		"no-dot",         // no scope.action
		"unknown.action", // not in catalog
		"kv.read: ",      // whitespace qualifier
	} {
		require.Errorf(t, Validate(tc), "expected %q to be rejected", tc)
	}
}

func TestAllowed_ExactAndWildcard(t *testing.T) {
	t.Parallel()
	grants := []string{
		"kv.read:cache",          // exact
		"kv.read:*",              // wildcard qualifier on kv.read
		"events.listen:dns.rec*", // malformed wildcard — should not match anything unintended
		"events.listen:dns.record.*",
		"network.outbound",
		"kv.*", // scope-level wildcard — deliberately unsupported
	}
	cases := []struct {
		name     string
		request  string
		expected bool
	}{
		{"exact match", "kv.read:cache", true},
		{"wildcard qualifier", "kv.read:state", true},
		{"action differs under same scope", "kv.write:cache", false},
		{"listen wildcard deep", "events.listen:dns.record.created", true},
		{"listen wildcard different topic", "events.listen:dns.zone.created", false},
		{"plain capability exact", "network.outbound", true},
		{"plain capability missing", "job.schedule", false},
		{"empty request", "", false},
		{"malformed grant does not over-match", "events.listen:dns.recordx", false},
		{"scope wildcard not honoured", "kv.delete:cache", false},
		{"qualifier empty after wildcard", "kv.read:", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, Allowed(grants, tc.request))
		})
	}
}

func TestIsKnownCapability(t *testing.T) {
	t.Parallel()
	assert.True(t, IsKnownCapability(CapKVRead))
	assert.True(t, IsKnownCapability("kv.read:cache"))
	assert.False(t, IsKnownCapability("unknown.action"))
}

func TestAllCapabilities_ReturnsACopy(t *testing.T) {
	t.Parallel()
	a := AllCapabilities()
	a[0] = "MUTATED"
	b := AllCapabilities()
	require.NotEqual(t, a[0], b[0], "AllCapabilities must return a defensive copy")
}

func TestMapEnforcer_GrantRevokeAllowed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	plugin := uuid.New()
	e := NewMapEnforcer()

	// Empty grant set: nothing allowed.
	ok, err := e.Allowed(ctx, plugin, CapKVRead)
	require.NoError(t, err)
	assert.False(t, ok)

	// Grant + re-check.
	e.Grant(plugin, "kv.read:cache")
	ok, err = e.Allowed(ctx, plugin, "kv.read:cache")
	require.NoError(t, err)
	assert.True(t, ok)

	// Wildcard grant broadens.
	e.Grant(plugin, "events.listen:dns.record.*")
	ok, err = e.Allowed(ctx, plugin, "events.listen:dns.record.created")
	require.NoError(t, err)
	assert.True(t, ok)

	// Revoke removes the exact slug only.
	e.Revoke(plugin, "kv.read:cache")
	ok, err = e.Allowed(ctx, plugin, "kv.read:cache")
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = e.Allowed(ctx, plugin, "events.listen:dns.record.created")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestMapEnforcer_GrantIsIdempotent(t *testing.T) {
	t.Parallel()
	plugin := uuid.New()
	e := NewMapEnforcer()
	e.Grant(plugin, "kv.read:cache", "kv.read:cache", "kv.read:cache")
	e.Grant(plugin, "kv.read:cache")
	// Internals: only one entry per slug.
	e.mu.RLock()
	defer e.mu.RUnlock()
	count := 0
	for _, s := range e.set[plugin] {
		if s == "kv.read:cache" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}
