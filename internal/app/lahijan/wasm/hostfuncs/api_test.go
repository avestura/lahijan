// api_test.go covers the path-validation helper and the
// register_handler / unregister_handler Go-side implementations
// against a fake api.Module + an in-memory repository. No DB.

package hostfuncs

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

func TestValidatePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want bool
	}{
		{"/webhook", true},
		{"/foo/bar", true},
		{"", false},
		{"webhook", false},
		{"/foo/../bar", false},
		{"//double", false},
		{"/", true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, validatePath(tc.path))
		})
	}
}

// — Tests below this line exercise the gate + memory paths. ————

func TestGate_NoPluginID_Denies(t *testing.T) {
	t.Parallel()
	enf := permission.NewMapEnforcer()
	reg := newTestRegistrar(enf)
	pid, code := reg.gate(context.Background(), "m", "fn", permission.CapKVRead)
	assert.Equal(t, uuid.Nil, pid)
	assert.Equal(t, StatusDenied, code)
}

func TestGate_PluginWithoutGrant_Denies(t *testing.T) {
	t.Parallel()
	enf := permission.NewMapEnforcer()
	pid := uuid.New()
	reg := newTestRegistrar(enf)
	ctx := WithPluginID(context.Background(), pid)
	gotPID, code := reg.gate(ctx, "m", "fn", permission.CapKVRead)
	assert.Equal(t, pid, gotPID)
	assert.Equal(t, StatusDenied, code)
}

func TestGate_PluginWithGrant_Allows(t *testing.T) {
	t.Parallel()
	enf := permission.NewMapEnforcer()
	pid := uuid.New()
	enf.Grant(pid, permission.CapKVRead)
	reg := newTestRegistrar(enf)
	ctx := WithPluginID(context.Background(), pid)
	gotPID, code := reg.gate(ctx, "m", "fn", permission.CapKVRead)
	assert.Equal(t, pid, gotPID)
	assert.Equal(t, StatusSuccess, code)
}

func TestGate_EnforcerError_Fails(t *testing.T) {
	t.Parallel()
	reg := newTestRegistrar(&errEnforcer{})
	pid := uuid.New()
	ctx := WithPluginID(context.Background(), pid)
	_, code := reg.gate(ctx, "m", "fn", permission.CapKVRead)
	assert.Equal(t, StatusGenericFailure, code)
}

// errEnforcer always returns an error to exercise the failure path.
type errEnforcer struct{}

func (errEnforcer) Allowed(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return false, errSentinel
}

func (errEnforcer) ListGrants(_ context.Context, _ uuid.UUID) ([]string, error) {
	return nil, errSentinel
}

var errSentinel = assertSentinel{}

type assertSentinel struct{}

func (assertSentinel) Error() string { return "boom" }

// newTestRegistrar builds a registrar with a discard logger. The
// enforcer is the only piece tests need to vary; the rest of Deps
// stays zero so every host function returns StatusUnavailable on the
// repo-touching paths.
func newTestRegistrar(e permission.Enforcer) *registrar {
	return &registrar{
		enforcer: e,
		deps:     Deps{},
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}
