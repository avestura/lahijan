package i18n

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultLocale(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "en", LocaleFromContext(context.Background()))
}

func TestT_DefaultLocale(t *testing.T) {
	t.Parallel()
	ctx := WithLocale(context.Background(), "en")
	got := T(ctx, "auth.err_invalid_credentials", nil)
	assert.Equal(t, "Invalid email or password.", got)
}

func TestT_FarsiLocale(t *testing.T) {
	t.Parallel()
	ctx := WithLocale(context.Background(), "fa")
	got := T(ctx, "auth.err_invalid_credentials", nil)
	// Farsi translation must be the actual fa.json value (not the id and not
	// the English fallback), proving fa.json loaded into the bundle.
	assert.Equal(t, "رایانشانی یا گذرواژه نادرست است.", got)
}

func TestT_Interpolation(t *testing.T) {
	t.Parallel()
	ctx := WithLocale(context.Background(), "en")
	got := T(ctx, "auth.err_password_too_weak", map[string]any{"Min": 12})
	assert.Contains(t, got, "12")
}

func TestT_UnknownKeyReturnsKey(t *testing.T) {
	t.Parallel()
	ctx := WithLocale(context.Background(), "en")
	// A missing id must surface the id itself, not crash or return "".
	assert.Equal(t, "auth.does_not_exist", T(ctx, "auth.does_not_exist", nil))
}

func TestT_FallsBackForUnknownLocale(t *testing.T) {
	t.Parallel()
	ctx := WithLocale(context.Background(), "zh-CN") // no bundle shipped
	got := T(ctx, "auth.err_invalid_credentials", nil)
	// Unknown locale falls back to the default English message.
	assert.Equal(t, "Invalid email or password.", got)
}

func TestResolveAcceptLanguage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		header string
		want   string
	}{
		{"", "en"},
		{"en-US,en;q=0.9", "en"},
		{"fa-IR,fa;q=0.9", "fa"},
		{"fa", "fa"},
		{"fr-FR", "en"}, // unsupported -> default
		{"garbage,,,", "en"},
	}
	for _, tc := range cases {
		got := ResolveAcceptLanguage(tc.header)
		require.Equalf(t, tc.want, got, "header=%q", tc.header)
	}
}
