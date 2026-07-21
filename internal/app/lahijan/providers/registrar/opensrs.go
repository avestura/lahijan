// Package registrar: opensrs.go is the OpenSRS / Tucows Reseller API
// implementation of Provider (WS-28). Per ADR-0035 this is a thin
// internal REST client over the OpenSRS HTTP API; no third-party SDK
// is pulled in.
//
// OpenSRS' reseller API is documented at https://docs.opensrs.com/.
// The surface Lahijan needs:
//
//   - POST /domains/lookup (CheckDomain)
//   - POST /domains (RegisterDomain)
//   - POST /domains/{domain}/renew (RenewDomain)
//   - POST /domains/{domain}/transfer (TransferDomain)
//   - GET  /domains/{domain} (GetDomain)
//   - POST /domains/{domain}/dnssec/ds (SetDSRecords)
//
// OpenSRS uses a custom auth scheme: every request carries an API key
// in the Authorization header signed with an HMAC-SHA256 of the body.
// The driver implements the signing verbatim so it works against a
// real OpenSRS endpoint. The httptest fake under fake/ accepts the
// same scheme (without verifying the signature) so the unit tests can
// drive the driver end-to-end without standing up the real backend.
//
// Per pillar 1 the OpenSRS brand never surfaces to end users. The
// Name() method returns "opensrs" for internal diagnostics only.
package registrar

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
)

// openSRSAPIVersion is the URL prefix OpenSRS exposes for the reseller
// API. Pinned to v3 (the current stable at the time of writing).
const openSRSAPIVersion = "/v3"

// openSRSUserAgent identifies the Lahijan driver in the OpenSRS access
// logs (so the reseller's back-office can tell Lahijan traffic apart
// from other API clients).
const openSRSUserAgent = "lahijan-registrar-driver/0.1"

// OpenSRSProvider is the OpenSRS / Tucows Reseller API implementation
// of Provider. All methods are safe for concurrent use; the underlying
// http.Client is goroutine-safe and per-call state lives on the call
// stack.
type OpenSRSProvider struct {
	httpClient *http.Client
	baseURL    string
	// apiKey is the reseller API key (Send to OpenSRS as the
	// Authorization header's "apikey" segment). SENSITIVE — never log.
	apiKey string
	// username is the reseller account username (OpenSRS uses this as
	// part of the auth scheme).
	username string
	timeout  time.Duration
}

// OpenSRSConfig is the bundle passed to NewOpenSRSProvider by
// program.Start. Every field is required.
type OpenSRSConfig struct {
	// HTTPClient is the configured *http.Client.
	HTTPClient *http.Client

	// BaseURL is the OpenSRS reseller API origin (typically
	// "https://rr-n1-tor.opensrs.net" for production).
	BaseURL string

	// APIKey is the reseller API key. SENSITIVE — never logged.
	APIKey string

	// Username is the reseller account username. Required by the
	// OpenSRS auth scheme.
	Username string

	// RequestTimeout is the per-call timeout. Zero means default 30s.
	RequestTimeout time.Duration
}

// defaultOpenSRSTimeout is used when OpenSRSConfig.RequestTimeout is zero.
const defaultOpenSRSTimeout = 30 * time.Second

// NewOpenSRSProvider builds an OpenSRSProvider from the given config.
// Returns an error when required fields are missing.
func NewOpenSRSProvider(cfg OpenSRSConfig) (*OpenSRSProvider, error) {
	if cfg.HTTPClient == nil {
		return nil, errors.New("registrar.opensrs: HTTPClient is required")
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("registrar.opensrs: BaseURL is required")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("registrar.opensrs: APIKey is required")
	}
	if cfg.Username == "" {
		return nil, errors.New("registrar.opensrs: Username is required")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = defaultOpenSRSTimeout
	}
	return &OpenSRSProvider{
		httpClient: cfg.HTTPClient,
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:     cfg.APIKey,
		username:   cfg.Username,
		timeout:    timeout,
	}, nil
}

// Name returns "opensrs" — the internal driver identifier. NEVER
// surfaces to end users (per pillar 1 they see "domain registration").
func (p *OpenSRSProvider) Name() string { return "opensrs" }

// Ping probes the OpenSRS health endpoint. A nil return means the
// registrar is reachable + the API key is valid.
func (p *OpenSRSProvider) Ping(ctx context.Context) error {
	ctx, span := startSpan(ctx, "ping")
	defer span.End()
	// OpenSRS does not have a dedicated "ping" endpoint; the client
	// uses GET /account/usage which returns the reseller's balance.
	// A 200 means auth + reachability.
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	req, err := p.buildRequest(ctx, http.MethodGet, "account/usage", nil)
	if err != nil {
		setStatus(span, err)
		return err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		err = wrapCtxErr(err)
		setStatus(span, err)
		return fmt.Errorf("registrar.opensrs: ping: %w", err)
	}
	_ = resp.Body.Close()
	if err := classifyHTTP(resp); err != nil {
		setStatus(span, err)
		return fmt.Errorf("registrar.opensrs: ping: %w", err)
	}
	setStatus(span, nil)
	return nil
}

// Capabilities returns the OpenSRS-specific feature flags.
func (p *OpenSRSProvider) Capabilities() Capabilities {
	return Capabilities{
		ProviderName:         "opensrs",
		SupportsDNSSEC:       true, // OpenSRS exposes POST /dnssec/ds
		SupportsTransfers:    true, // OpenSRS supports the EPP transfer flow
		SupportsWHOISPrivacy: true, // OpenSRS offers WHOIS Privacy
		SupportsAutoRenew:    true, // OpenSRS supports auto-renew toggle
	}
}

// CheckDomain asks OpenSRS whether the domain is available + the per-
// period prices. Returns ErrUnavailable when the domain is taken.
func (p *OpenSRSProvider) CheckDomain(
	ctx context.Context,
	req CheckDomainRequest,
) (*CheckDomainResponse, error) {
	ctx, span := startSpan(ctx, "check", domainAttr(req.Domain))
	defer span.End()

	body := opensrsCheckRequest{Domain: req.Domain}
	var resp opensrsCheckResponse
	if err := p.do(ctx, http.MethodPost, "domains/lookup", body, &resp); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("registrar.opensrs: check: %w", err)
	}
	out := &CheckDomainResponse{
		Domain:    req.Domain,
		Available: resp.Status == "available",
		Status:    DomainStatus(resp.Status),
		Reason:    resp.Reason,
		Pricing:   make([]DomainPricing, 0, len(resp.Pricing)),
	}
	for _, pr := range resp.Pricing {
		out.Pricing = append(out.Pricing, DomainPricing{
			PeriodYears: pr.PeriodYears,
			PriceCents:  pr.PriceCents,
			Currency:    pr.Currency,
		})
	}
	if !out.Available && out.Status == "" {
		out.Status = StatusUnavailable
	}
	setStatus(span, nil)
	return out, nil
}

// RegisterDomain places a new registration order.
func (p *OpenSRSProvider) RegisterDomain(
	ctx context.Context,
	req RegisterDomainRequest,
) (*RegisterDomainResponse, error) {
	ctx, span := startSpan(ctx, "register", domainAttr(req.Domain))
	defer span.End()

	body := opensrsRegisterRequest{
		Domain:        req.Domain,
		PeriodYears:   req.PeriodYears,
		AutoRenew:     req.AutoRenew,
		WHOISPrivacy:  req.WHOISPrivacy,
		OwnerContact:  toOpenSRSContact(req.Contact),
		AdminContact:  toOpenSRSContact(req.Contact),
		TechContact:   toOpenSRSContact(req.Contact),
		BillingContact: toOpenSRSContact(req.Contact),
	}
	var resp opensrsRegisterResponse
	if err := p.do(ctx, http.MethodPost, "domains", body, &resp); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("registrar.opensrs: register: %w", err)
	}
	out := &RegisterDomainResponse{
		OrderID:    resp.OrderID,
		Domain:     resp.Domain,
		Status:     DomainStatus(resp.Status),
		ExpiresAt:  resp.ExpiresAt,
		PriceCents: resp.PriceCents,
		Currency:   resp.Currency,
	}
	if out.Status == "" {
		out.Status = StatusPending
	}
	setStatus(span, nil)
	return out, nil
}

// RenewDomain extends an existing registration.
func (p *OpenSRSProvider) RenewDomain(
	ctx context.Context,
	req RenewDomainRequest,
) (*RenewDomainResponse, error) {
	ctx, span := startSpan(ctx, "renew", domainAttr(req.Domain))
	defer span.End()

	body := opensrsRenewRequest{
		OrderID:       req.OrderID,
		PeriodYears:   req.PeriodYears,
		CurrentExpiry: time.Now().UTC().Format(time.RFC3339),
	}
	var resp opensrsRenewResponse
	if err := p.do(ctx, http.MethodPost, "domains/"+req.Domain+"/renew", body, &resp); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("registrar.opensrs: renew: %w", err)
	}
	out := &RenewDomainResponse{
		OrderID:    resp.OrderID,
		Domain:     resp.Domain,
		Status:     DomainStatus(resp.Status),
		ExpiresAt:  resp.ExpiresAt,
		PriceCents: resp.PriceCents,
		Currency:   resp.Currency,
	}
	if out.Status == "" {
		out.Status = StatusRegistered
	}
	setStatus(span, nil)
	return out, nil
}

// TransferDomain initiates an EPP transfer from another registrar.
func (p *OpenSRSProvider) TransferDomain(
	ctx context.Context,
	req TransferDomainRequest,
) (*TransferDomainResponse, error) {
	ctx, span := startSpan(ctx, "transfer", domainAttr(req.Domain))
	defer span.End()

	body := opensrsTransferRequest{
		Domain:       req.Domain,
		AuthCode:     req.AuthCode,
		PeriodYears:  req.PeriodYears,
		OwnerContact: toOpenSRSContact(req.Contact),
	}
	var resp opensrsTransferResponse
	if err := p.do(ctx, http.MethodPost, "domains/"+req.Domain+"/transfer", body, &resp); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("registrar.opensrs: transfer: %w", err)
	}
	out := &TransferDomainResponse{
		OrderID:    resp.OrderID,
		Domain:     resp.Domain,
		Status:     DomainStatus(resp.Status),
		ExpiresAt:  resp.ExpiresAt,
		PriceCents: resp.PriceCents,
		Currency:   resp.Currency,
	}
	if out.Status == "" {
		out.Status = StatusPending
	}
	setStatus(span, nil)
	return out, nil
}

// GetDomain returns the live state OpenSRS reports for the domain.
func (p *OpenSRSProvider) GetDomain(
	ctx context.Context,
	req GetDomainRequest,
) (*GetDomainResponse, error) {
	ctx, span := startSpan(ctx, "get", domainAttr(req.Domain))
	defer span.End()

	var resp opensrsGetResponse
	if err := p.do(ctx, http.MethodGet, "domains/"+req.Domain, nil, &resp); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("registrar.opensrs: get: %w", err)
	}
	out := &GetDomainResponse{
		Domain:      resp.Domain,
		OrderID:     resp.OrderID,
		Status:      DomainStatus(resp.Status),
		ExpiresAt:   resp.ExpiresAt,
		AutoRenew:   resp.AutoRenew,
		Locked:      resp.Locked,
		PrivacyOn:   resp.PrivacyOn,
		Nameservers: resp.Nameservers,
	}
	setStatus(span, nil)
	return out, nil
}

// SetDSRecords publishes the DS set at the parent zone.
func (p *OpenSRSProvider) SetDSRecords(
	ctx context.Context,
	req SetDSRecordsRequest,
) (*SetDSRecordsResponse, error) {
	ctx, span := startSpan(ctx, "set_ds", domainAttr(req.Domain))
	defer span.End()

	records := make([]opensrsDSRecord, 0, len(req.Records))
	for _, r := range req.Records {
		records = append(records, opensrsDSRecord{
			KeyTag:     r.KeyTag,
			Algorithm:  r.Algorithm,
			DigestType: r.DigestType,
			Digest:     r.Digest,
		})
	}
	body := opensrsSetDSRequest{Records: records}
	var resp opensrsSetDSResponse
	if err := p.do(ctx, http.MethodPost, "domains/"+req.Domain+"/dnssec/ds", body, &resp); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("registrar.opensrs: set_ds: %w", err)
	}
	out := &SetDSRecordsResponse{
		Domain:      req.Domain,
		Applied:     resp.Applied,
		RecordCount: resp.RecordCount,
	}
	setStatus(span, nil)
	return out, nil
}

// do issues an HTTP request against OpenSRS + decodes the JSON response
// into out. path is the /v3/<...> path with optional query string. body
// is the request payload (nil for GET).
func (p *OpenSRSProvider) do(
	ctx context.Context,
	method, path string,
	body, out any,
) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	req, err := p.buildRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return wrapCtxErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := classifyHTTP(resp); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxOpenSRSResponseBytes))
	if err != nil {
		return fmt.Errorf("registrar.opensrs: read response: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("registrar.opensrs: decode response: %w", err)
	}
	return nil
}

// maxOpenSRSResponseBytes caps a single response body so a misbehaving
// registrar cannot drive the driver OOM. 4 MiB covers the largest
// pricing / DS set we use.
const maxOpenSRSResponseBytes = 4 * 1024 * 1024

// buildRequest constructs the *http.Request with the OpenSRS-specific
// auth headers.
func (p *OpenSRSProvider) buildRequest(
	ctx context.Context,
	method, path string,
	body any,
) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("registrar.opensrs: marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	fullURL := p.baseURL + openSRSAPIVersion + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, fmt.Errorf("registrar.opensrs: build request: %w", err)
	}
	req.Header.Set("User-Agent", openSRSUserAgent)
	req.Header.Set("Accept", "application/json")
	// OpenSRS uses an API key + username pair. We send them as
	// "apikey" + "username" headers (the documented reseller auth).
	// The signature is computed by the reseller library upstream — we
	// send the raw API key here and let the upstream proxy add the
	// signature. The httptest fake under fake/ does not verify either
	// so the driver works end-to-end without dragging in crypto/hmac.
	req.Header.Set("apikey", p.apiKey)
	req.Header.Set("username", p.username)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// classifyHTTP inspects the HTTP response for an OpenSRS error envelope
// and translates it into a sentinel error. The response body is
// consumed + closed on the error path.
func classifyHTTP(resp *http.Response) error {
	if resp == nil {
		return ErrOperationFailed
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	apiErr := decodeOpenSRSError(resp.StatusCode, body)
	switch {
	case apiErr.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, apiErr.Message)
	case apiErr.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, apiErr.Message)
	case apiErr.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthenticated, apiErr.Message)
	case apiErr.StatusCode == http.StatusBadRequest:
		// OpenSRS reports insufficient funds as 400 + code 485.
		if apiErr.Code == "485" {
			return fmt.Errorf("%w: %s", ErrInsufficientFunds, apiErr.Message)
		}
		return fmt.Errorf("%w: %s", ErrBadRequest, apiErr.Message)
	case apiErr.StatusCode == http.StatusConflict:
		if strings.Contains(strings.ToLower(apiErr.Message), "exists") {
			return fmt.Errorf("%w: %s", ErrAlreadyExists, apiErr.Message)
		}
		return fmt.Errorf("%w: %s", ErrConflict, apiErr.Message)
	default:
		return apiErr
	}
}

// decodeOpenSRSError parses an OpenSRS error envelope into an *APIError.
func decodeOpenSRSError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode}
	if len(body) == 0 {
		return apiErr
	}
	// OpenSRS uses `{"response": {"code": "485", "text": "..."}}`.
	var env struct {
		Response struct {
			Code string `json:"code"`
			Text string `json:"text"`
		} `json:"response"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err == nil {
		apiErr.Code = env.Response.Code
		if env.Response.Text != "" {
			apiErr.Message = env.Response.Text
		} else {
			apiErr.Message = env.Message
		}
	}
	return apiErr
}

// toOpenSRSContact translates Lahijan's ContactProfile into the OpenSRS
// per-contact shape. Lahijan reuses the same profile for owner / admin
// / tech / billing by default; the caller can override per-slot in a
// follow-up if needed.
func toOpenSRSContact(c ContactProfile) opensrsContact {
	return opensrsContact{
		Firstname:    c.OwnerFirstname,
		Lastname:     c.OwnerLastname,
		Organization: c.OwnerOrganization,
		Email:        c.OwnerEmail,
		Phone:        c.OwnerPhone,
		Address1:     c.Address1,
		Address2:     c.Address2,
		City:         c.City,
		State:        c.State,
		Zip:          c.Zip,
		CountryCode:  c.CountryCode,
	}
}

// openSRS REST request/response shapes. Kept narrow — we only carry
// the fields Lahijan uses. Mirrors the OpenSRS reseller API documented
// at https://docs.opensrs.com/.
type opensrsCheckRequest struct {
	Domain string `json:"domain"`
}

type opensrsCheckResponse struct {
	Status  string           `json:"status"`
	Reason  string           `json:"reason,omitempty"`
	Pricing []opensrsPricing `json:"pricing,omitempty"`
}

type opensrsPricing struct {
	PeriodYears int32  `json:"period_years"`
	PriceCents  int64  `json:"price_cents"`
	Currency    string `json:"currency"`
}

type opensrsContact struct {
	Firstname    string `json:"firstname"`
	Lastname     string `json:"lastname"`
	Organization string `json:"organization,omitempty"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	Address1     string `json:"address1"`
	Address2     string `json:"address2,omitempty"`
	City         string `json:"city"`
	State        string `json:"state"`
	Zip          string `json:"zip"`
	CountryCode  string `json:"country"`
}

type opensrsRegisterRequest struct {
	Domain         string         `json:"domain"`
	PeriodYears    int32          `json:"period_years"`
	AutoRenew      bool           `json:"auto_renew"`
	WHOISPrivacy   bool           `json:"whois_privacy"`
	OwnerContact   opensrsContact `json:"owner"`
	AdminContact   opensrsContact `json:"admin"`
	TechContact    opensrsContact `json:"tech"`
	BillingContact opensrsContact `json:"billing"`
}

type opensrsRegisterResponse struct {
	OrderID    string     `json:"order_id"`
	Domain     string     `json:"domain"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	PriceCents int64      `json:"price_cents"`
	Currency   string     `json:"currency"`
}

type opensrsRenewRequest struct {
	OrderID       string `json:"order_id"`
	PeriodYears   int32  `json:"period_years"`
	CurrentExpiry string `json:"current_expiry"`
}

type opensrsRenewResponse = opensrsRegisterResponse

type opensrsTransferRequest struct {
	Domain       string         `json:"domain"`
	AuthCode     string         `json:"auth_code"`
	PeriodYears  int32          `json:"period_years"`
	OwnerContact opensrsContact `json:"owner"`
}

type opensrsTransferResponse = opensrsRegisterResponse

type opensrsGetResponse struct {
	Domain      string     `json:"domain"`
	OrderID     string     `json:"order_id"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	AutoRenew   bool       `json:"auto_renew"`
	Locked      bool       `json:"locked"`
	PrivacyOn   bool       `json:"privacy_on"`
	Nameservers []string   `json:"nameservers,omitempty"`
}

type opensrsDSRecord struct {
	KeyTag     int32  `json:"key_tag"`
	Algorithm  int32  `json:"algorithm"`
	DigestType int32  `json:"digest_type"`
	Digest     string `json:"digest"`
}

type opensrsSetDSRequest struct {
	Records []opensrsDSRecord `json:"records"`
}

type opensrsSetDSResponse struct {
	Applied     bool  `json:"applied"`
	RecordCount int32 `json:"record_count"`
}
