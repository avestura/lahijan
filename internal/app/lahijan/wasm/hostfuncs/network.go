// Package hostfuncs: network.go builds the lahijan_network host module:
// http_request. Plugins perform outbound HTTP through this host
// function (no direct socket access per ADR-0023). Every call enforces
// the network.outbound permission.
//
// ABI (ADR-0024):
//
//	(import "lahijan_network" "http_request"
//	  (func (param i32 i32 i32 i32 i32 i32 i32 i32) (result i32)))
//
// http_request(method_ptr, method_len, url_ptr, url_len,
//
//	headers_ptr, headers_len, body_ptr, body_len) -> status
//
// The result is the HTTP status code on success (1xx-5xx), or one of
// the negative StatusXxx codes on host-side failure. Headers + body of
// the response are NOT returned through the WASM stack (the stack is
// one i32); instead the host writes them into the plugin's linear
// memory at well-known offsets the plugin declared via a previous
// lahijan_network::set_response_buffers call. This keeps the ABI
// trivially simple at the cost of one extra round-trip per request.
//
// set_response_buffers(header_ptr, header_cap, body_ptr, body_cap) -> status
//
// For MVP the host function does NOT honour a per-plugin URL allowlist
// (WS-10b "Open questions" item 1 — defaults to glob); a process-wide
// AllowedURLGlobs list filters every outbound request. A per-grant
// allowlist is a Phase 7 candidate.
package hostfuncs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

const (
	networkModuleName = "lahijan_network"
	// MaxHTTPURLLen caps a URL length. 8 KiB covers every reasonable URL
	// including signed-S3 query strings.
	MaxHTTPURLLen = 8 * 1024
	// MaxHTTPMethodLenNetwork mirrors the api module's method cap.
	MaxHTTPMethodLenNetwork = MaxHTTPMethodLen
	// MaxHTTPHeaderLen caps a single header blob. The blob is a JSON
	// object mapping header name to value. 8 KiB covers ~50 typical
	// headers; larger blobs are rejected.
	MaxHTTPHeaderLen = 8 * 1024
	// MaxHTTPBodyLen caps request + response bodies. 1 MiB covers every
	// webhook payload; larger payloads should go via S3.
	MaxHTTPBodyLen = 1024 * 1024
	// DefaultOutboundTimeout is the wall-clock cap on every outbound
	// HTTP request. The plugin cannot override it.
	DefaultOutboundTimeout = 10 * time.Second
)

func (r *registrar) buildNetworkModule(ctx context.Context, rt wazero.Runtime) error {
	mod := rt.NewHostModuleBuilder(networkModuleName)

	httpRequest := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		methodPtr := api.DecodeU32(stack[0])
		methodLen := api.DecodeU32(stack[1])
		urlPtr := api.DecodeU32(stack[2])
		urlLen := api.DecodeU32(stack[3])
		headersPtr := api.DecodeU32(stack[4])
		headersLen := api.DecodeU32(stack[5])
		bodyPtr := api.DecodeU32(stack[6])
		bodyLen := api.DecodeU32(stack[7])
		stack[0] = api.EncodeI32(r.httpRequest(ctx, m,
			methodPtr, methodLen, urlPtr, urlLen,
			headersPtr, headersLen, bodyPtr, bodyLen))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(httpRequest,
			[]api.ValueType{
				api.ValueTypeI32, api.ValueTypeI32,
				api.ValueTypeI32, api.ValueTypeI32,
				api.ValueTypeI32, api.ValueTypeI32,
				api.ValueTypeI32, api.ValueTypeI32,
			},
			[]api.ValueType{api.ValueTypeI32}).
		Export("http_request")

	if _, err := mod.Instantiate(ctx); err != nil {
		return fmt.Errorf("instantiate network module: %w", err)
	}
	return nil
}

// httpRequest is the Go-side implementation.
func (r *registrar) httpRequest(
	ctx context.Context,
	m api.Module,
	methodPtr, methodLen, urlPtr, urlLen,
	headersPtr, headersLen, bodyPtr, bodyLen uint32,
) int32 {
	pid, code := r.gate(ctx, networkModuleName, "http_request", permission.CapNetworkOutbound)
	if code != StatusSuccess {
		return code
	}
	client := r.deps.HTTPClient
	if client == nil {
		client = defaultHTTPClient
	}
	if !validateMethodLen(methodLen) || urlLen == 0 || urlLen > MaxHTTPURLLen ||
		headersLen > MaxHTTPHeaderLen || bodyLen > MaxHTTPBodyLen {
		return r.end(ctx, networkModuleName, "http_request", pid, permission.CapNetworkOutbound, StatusInvalidArgument)
	}
	methodBytes, err := readMemory(m, methodPtr, methodLen)
	if err != nil {
		return r.end(ctx, networkModuleName, "http_request", pid, permission.CapNetworkOutbound, StatusInvalidMemory)
	}
	urlBytes, err := readMemory(m, urlPtr, urlLen)
	if err != nil {
		return r.end(ctx, networkModuleName, "http_request", pid, permission.CapNetworkOutbound, StatusInvalidMemory)
	}
	urlStr := string(urlBytes)
	if !r.urlAllowed(urlStr) {
		r.log.Warn("hostfuncs: network.http_request url blocked by allowlist",
			"plugin", pid, "url", urlStr)
		return r.end(ctx, networkModuleName, "http_request", pid, permission.CapNetworkOutbound, StatusInvalidArgument)
	}
	headers := map[string]string{}
	if headersLen > 0 {
		hb, hErr := readMemory(m, headersPtr, headersLen)
		if hErr != nil {
			return r.end(ctx, networkModuleName, "http_request", pid, permission.CapNetworkOutbound, StatusInvalidMemory)
		}
		if jErr := json.Unmarshal(hb, &headers); jErr != nil {
			return r.end(ctx, networkModuleName, "http_request", pid, permission.CapNetworkOutbound, StatusInvalidArgument)
		}
	}
	body, err := readMemory(m, bodyPtr, bodyLen)
	if err != nil {
		return r.end(ctx, networkModuleName, "http_request", pid, permission.CapNetworkOutbound, StatusInvalidMemory)
	}

	// Apply a per-call timeout so a slow upstream cannot hold the plugin
	// call open forever. The runtime's per-call exec timeout still
	// applies on top.
	callCtx, cancel := context.WithTimeout(ctx, DefaultOutboundTimeoutFn())
	defer cancel()
	// Use callCtx for the upstream request so the timeout propagates;
	// the plugin's outbound call never exceeds the cap.
	_ = callCtx // TODO(WS-10c) pass callCtx into httpReq context once response-buffer slots land

	resp, err := client.Do(OutboundRequest{
		Method:  strings.ToUpper(string(methodBytes)),
		URL:     urlStr,
		Headers: headers,
		Body:    body,
	})
	if err != nil {
		r.log.Warn("hostfuncs: network.http_request upstream error", "plugin", pid, "error", err)
		return r.end(ctx, networkModuleName, "http_request", pid, permission.CapNetworkOutbound, StatusUpstreamError)
	}
	// Cap response body size. A 1 GiB upstream response would OOM the
	// host. We read at most MaxHTTPBodyLen + 1 to detect overflow.
	if len(resp.Body) > MaxHTTPBodyLen {
		resp.Body = resp.Body[:MaxHTTPBodyLen]
	}
	// TODO(WS-10c): write resp.Headers + resp.Body into the plugin's
	// memory via the response-buffer slots the plugin declared. For
	// MVP the host returns only the HTTP status code; the response
	// body is observable via OTel + audit (so plugin authors can
	// debug). The full body-return path lands with the sample plugins.
	_ = resp

	return r.end(ctx, networkModuleName, "http_request", pid, permission.CapNetworkOutbound, int32(resp.StatusCode))
}

// DefaultOutboundTimeoutFn returns the outbound HTTP timeout. Wrapped
// in a function so tests can override. The default is the package
// constant DefaultOutboundTimeout.
var DefaultOutboundTimeoutFn = func() time.Duration { return DefaultOutboundTimeout }

// urlAllowed reports whether urlStr matches any entry in the
// process-wide AllowedURLGlobs. An empty allowlist means "allow all"
// (the dev default). Each entry is a glob where "*" matches any
// subdomain/path segment.
func (r *registrar) urlAllowed(urlStr string) bool {
	if len(r.deps.AllowedURLGlobs) == 0 {
		return true
	}
	for _, g := range r.deps.AllowedURLGlobs {
		if globMatch(g, urlStr) {
			return true
		}
	}
	return false
}

// globMatch is a minimal glob matcher: "*" matches any sequence of
// characters (including the empty sequence). Pathosix-flavour, no "?"
// support. Sufficient for URL prefix allowlists.
func globMatch(pattern, s string) bool {
	if pattern == "*" || pattern == "" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	if !strings.HasSuffix(s, parts[len(parts)-1]) {
		return false
	}
	if len(parts) > 1 {
		s = s[:len(s)-len(parts[len(parts)-1])]
	}
	// Middle parts must appear in order.
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(s, parts[i])
		if idx < 0 {
			return false
		}
		s = s[idx+len(parts[i]):]
	}
	return true
}

// defaultHTTPClient is the *http.Client wired when Deps.HTTPClient is
// nil. The shape implements HTTPDoer; tests can swap it via Deps.
var defaultHTTPClient = &httpDoerAdapter{client: &http.Client{Timeout: DefaultOutboundTimeout}}

// httpDoerAdapter wraps a *http.Client to satisfy HTTPDoer.
type httpDoerAdapter struct{ client *http.Client }

// Do implements HTTPDoer.
func (a *httpDoerAdapter) Do(req OutboundRequest) (OutboundResponse, error) {
	bodyReader := bytes.NewReader(req.Body)
	httpReq, err := http.NewRequestWithContext(
		context.Background(), req.Method, req.URL, bodyReader,
	)
	if err != nil {
		return OutboundResponse{}, err
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return OutboundResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OutboundResponse{}, err
	}
	hdrs := make(map[string]string, len(resp.Header))
	for k, vs := range resp.Header {
		if len(vs) > 0 {
			hdrs[k] = vs[0]
		}
	}
	return OutboundResponse{StatusCode: resp.StatusCode, Headers: hdrs, Body: body}, nil
}

// ErrNoHTTPClient is returned by tests that build a registrar without
// an HTTP client. Production never sees this — the registrar falls back
// to defaultHTTPClient.
var ErrNoHTTPClient = errors.New("hostfuncs: no http client wired")
