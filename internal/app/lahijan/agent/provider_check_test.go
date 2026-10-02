package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		model   string
		handler http.HandlerFunc
		want    ProviderCheckStatus
	}{
		{
			name:  "models listed and model present",
			model: "gpt-4o",
			handler: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer k", r.Header.Get("Authorization"))
				_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"}]}`))
			},
			want: ProviderCheckOK,
		},
		{
			name:  "model missing from list",
			model: "nope",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
			},
			want: ProviderCheckModelNotFound,
		},
		{
			name: "bad key",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			want: ProviderCheckAuthFailed,
		},
		{
			name:  "no models endpoint falls back to a completion",
			model: "m",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				require.Equal(t, "/v1/chat/completions", r.URL.Path)
				_, _ = w.Write([]byte(`{}`))
			},
			want: ProviderCheckOK,
		},
		{
			name: "no models endpoint and no model to probe with",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			want: ProviderCheckBadResponse,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			want: ProviderCheckBadResponse,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tc.handler)
			t.Cleanup(srv.Close)
			got := probeProvider(context.Background(), srv.Client(),
				ResolvedProvider{Provider: "openai", Model: tc.model, BaseURL: srv.URL + "/v1", APIKey: "k"})
			require.Equal(t, tc.want, got.Status)
			require.Equal(t, tc.want == ProviderCheckOK, got.OK)
		})
	}
}

func TestProbeProviderUnreachable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	got := probeProvider(context.Background(), http.DefaultClient,
		ResolvedProvider{Provider: "openai", BaseURL: url, APIKey: "k"})
	require.Equal(t, ProviderCheckUnreachable, got.Status)
	require.Zero(t, got.HTTPStatus)
}

func TestModelsURL(t *testing.T) {
	t.Parallel()
	require.Equal(t, "https://api.openai.com/v1/models", modelsURL(ResolvedProvider{Provider: "openai"}))
	require.Equal(t, "http://x/v1/models", modelsURL(ResolvedProvider{BaseURL: "http://x/v1/chat/completions"}))
	require.Equal(t, "http://x/v1/models", modelsURL(ResolvedProvider{BaseURL: "http://x/v1/"}))
}
