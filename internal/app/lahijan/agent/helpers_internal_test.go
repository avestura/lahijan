package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

func TestTruncate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "abc", truncate("abc", 5))
	assert.Equal(t, "ab…", truncate("abcdef", 2))
}

func TestReadErrorBody_CapsAtOneKiB(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "rate limited", readErrorBody(strings.NewReader("rate limited")))
	assert.Len(t, readErrorBody(strings.NewReader(strings.Repeat("x", 4096))), 1024)
}

func TestErrJSON(t *testing.T) {
	t.Parallel()
	assert.JSONEq(t, `{}`, string(errJSON(nil)))
	assert.JSONEq(t, `{"error":"boom"}`, string(errJSON(errors.New("boom"))))
}

func TestPausedDefault_IsSingleton(t *testing.T) {
	t.Parallel()
	assert.Equal(t, pausedDefault(), pausedDefault())
}

func TestMarshalRows_WrapsCountAndItems(t *testing.T) {
	t.Parallel()
	raw, err := marshalRows([]int{1, 2, 3}, func(n int) int { return n * 2 })
	require.NoError(t, err)
	assert.JSONEq(t, `{"count":3,"items":[2,4,6]}`, string(raw))

	raw, err = marshalRows([]int(nil), func(n int) int { return n })
	require.NoError(t, err)
	assert.JSONEq(t, `{"count":0,"items":[]}`, string(raw))
}

func TestProjectors_ExposeOnlySafeColumns(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		got  map[string]any
		keys []string
	}{
		"zone":     {projectZone(gen.DnsZone{}), []string{"id", "name", "kind", "is_dnssec_enabled", "is_axfr_enabled", "description"}},
		"instance": {projectInstance(gen.ComputeInstance{}), []string{"id", "name", "type", "status", "image", "profiles", "description"}},
		"bucket": {projectBucket(gen.StorageBucket{}), []string{
			"id", "name", "label", "description", "quota_bytes", "quota_objects", "bytes_used", "objects_used", "versioning", "object_lock_enabled",
		}},
		"usage": {projectUsage(gen.UsageEvent{}), []string{"id", "resource_type", "qty", "unit", "started_at", "ended_at"}},
	}
	for name, tc := range cases {
		assert.Len(t, tc.got, len(tc.keys), name)
		for _, k := range tc.keys {
			assert.Contains(t, tc.got, k, name)
		}
		_, err := json.Marshal(tc.got)
		assert.NoError(t, err, name)
	}
}
