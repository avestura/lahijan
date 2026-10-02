// Package agent: provider_check.go implements the "test connection" probe for a
// BYOK model provider. It lets a user verify a base URL, key and model before
// (or after) saving a config, without starting an agent turn.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProviderCheckStatus classifies the outcome of a provider probe. The API
// returns it as a stable code and the UI translates it; the upstream error
// body is deliberately never echoed (the base URL is user-supplied, so
// reflecting responses would turn the probe into an SSRF read primitive).
type ProviderCheckStatus string

// Provider probe outcomes.
const (
	// ProviderCheckOK means the endpoint answered and accepted the key.
	ProviderCheckOK ProviderCheckStatus = "ok"
	// ProviderCheckAuthFailed means the endpoint rejected the key (401/403).
	ProviderCheckAuthFailed ProviderCheckStatus = "auth_failed"
	// ProviderCheckModelNotFound means the key works but the model is not offered.
	ProviderCheckModelNotFound ProviderCheckStatus = "model_not_found"
	// ProviderCheckUnreachable means no HTTP answer (DNS, TLS, refused, timeout).
	ProviderCheckUnreachable ProviderCheckStatus = "unreachable"
	// ProviderCheckBadResponse means the endpoint answered with something that
	// is not an OpenAI-compatible reply.
	ProviderCheckBadResponse ProviderCheckStatus = "bad_response"
)

// ErrProviderCheckInvalid is returned for an unusable probe request (missing
// provider, no key available, or a base URL that is not http/https).
var ErrProviderCheckInvalid = errors.New("agent: invalid provider check request")

// ProviderCheckInput is what to probe. When ProviderID is set and APIKey is
// empty, the stored (decrypted) key of that config is used, and unset fields
// fall back to the stored provider/model/base URL.
type ProviderCheckInput struct {
	Provider   string
	Model      string
	BaseURL    string
	APIKey     string
	ProviderID *uuid.UUID
}

// ProviderCheckResult is the probe outcome.
type ProviderCheckResult struct {
	OK         bool
	Status     ProviderCheckStatus
	HTTPStatus int // 0 when there was no HTTP answer
	LatencyMs  int64
}

const providerCheckTimeout = 15 * time.Second

// CheckProvider probes the provider endpoint. It first lists models (free, no
// tokens spent) and falls back to a one-token completion for endpoints that do
// not implement /models.
func (s *Service) CheckProvider(ctx context.Context, userID uuid.UUID, in ProviderCheckInput) (ProviderCheckResult, error) {
	if s.IsDisabled() {
		return ProviderCheckResult{}, ErrDisabled
	}
	p, err := s.providerForCheck(ctx, userID, in)
	if err != nil {
		return ProviderCheckResult{}, err
	}
	return probeProvider(ctx, &http.Client{Timeout: providerCheckTimeout}, p), nil
}

// providerForCheck merges the request with a stored config when asked to.
func (s *Service) providerForCheck(ctx context.Context, userID uuid.UUID, in ProviderCheckInput) (ResolvedProvider, error) {
	p := ResolvedProvider{Provider: in.Provider, Model: in.Model, BaseURL: in.BaseURL, APIKey: in.APIKey}
	if in.ProviderID != nil && in.APIKey == "" {
		row, err := s.repo.GetProviderConfig(ctx, userID, *in.ProviderID)
		if err != nil {
			return ResolvedProvider{}, ErrNotFound
		}
		key, err := s.openKey(row.ApiKeyEncrypted)
		if err != nil {
			return ResolvedProvider{}, fmt.Errorf("agent.provider.check: decrypt key: %w", err)
		}
		p.APIKey = key
		if p.Provider == "" {
			p.Provider = row.Provider
		}
		if p.Model == "" {
			p.Model = row.Model
		}
		if p.BaseURL == "" {
			p.BaseURL = row.BaseUrl
		}
	}
	if strings.TrimSpace(p.Provider) == "" || p.APIKey == "" {
		return ResolvedProvider{}, ErrProviderCheckInvalid
	}
	if p.BaseURL != "" {
		u, err := url.Parse(p.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return ResolvedProvider{}, ErrProviderCheckInvalid
		}
	}
	return p, nil
}

// probeProvider runs the HTTP probe. It never returns an error: every failure
// is folded into the result status.
func probeProvider(ctx context.Context, client *http.Client, p ResolvedProvider) ProviderCheckResult {
	start := time.Now()
	finish := func(st ProviderCheckStatus, code int) ProviderCheckResult {
		return ProviderCheckResult{
			OK: st == ProviderCheckOK, Status: st, HTTPStatus: code, LatencyMs: time.Since(start).Milliseconds(),
		}
	}

	status, body, err := doProbe(ctx, client, http.MethodGet, modelsURL(p), p.APIKey, nil)
	if err != nil {
		return finish(ProviderCheckUnreachable, 0)
	}
	switch {
	case status >= 200 && status < 300:
		if p.Model != "" {
			if ids, ok := parseModelIDs(body); ok && !containsFold(ids, p.Model) {
				return finish(ProviderCheckModelNotFound, status)
			}
		}
		return finish(ProviderCheckOK, status)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return finish(ProviderCheckAuthFailed, status)
	case status != http.StatusNotFound && status != http.StatusMethodNotAllowed:
		return finish(ProviderCheckBadResponse, status)
	}

	// No /models endpoint: spend one token on a completion instead.
	model := p.Model
	if model == "" {
		return finish(ProviderCheckBadResponse, status)
	}
	payload, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
	})
	status, _, err = doProbe(ctx, client, http.MethodPost, chatCompletionsURL(p), p.APIKey, payload)
	if err != nil {
		return finish(ProviderCheckUnreachable, 0)
	}
	switch {
	case status >= 200 && status < 300:
		return finish(ProviderCheckOK, status)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return finish(ProviderCheckAuthFailed, status)
	case status == http.StatusNotFound:
		return finish(ProviderCheckModelNotFound, status)
	default:
		return finish(ProviderCheckBadResponse, status)
	}
}

func doProbe(ctx context.Context, client *http.Client, method, target, key string, payload []byte) (int, []byte, error) {
	var rdr *bytes.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rdr)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("authorization", "Bearer "+key)
	req.Header.Set("accept", "application/json")
	if payload != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("probe request: %w", err)
	}
	defer resp.Body.Close()
	// Cap the read; a model list is small and we never reflect it. A read
	// error still leaves the status code, which is enough to classify.
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b, nil
}

// modelsURL resolves the OpenAI-style model list endpoint for a provider.
func modelsURL(p ResolvedProvider) string {
	base := strings.TrimSpace(p.BaseURL)
	if base == "" {
		base = defaultProviderBase(p.Provider)
	}
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(base, "/chat/completions")
	return strings.TrimRight(base, "/") + "/models"
}

// parseModelIDs extracts model ids from an OpenAI-style {"data":[{"id":..}]}.
func parseModelIDs(body []byte) ([]string, bool) {
	var doc struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || len(doc.Data) == 0 {
		return nil, false
	}
	ids := make([]string, 0, len(doc.Data))
	for _, d := range doc.Data {
		ids = append(ids, d.ID)
	}
	return ids, true
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
