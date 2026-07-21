// Package fake ships an in-process httptest server that mimics the
// OpenSRS / Tucows reseller API shape every WS-28 test exercises.
// Mirrors providers/powerdns/fake/ + providers/incus/fake/.
//
// The fake is intentionally narrow: it implements only the surface the
// registrar service tests touch (lookup / register / renew / transfer /
// get / dnssec set). It does NOT verify the API-key header (the unit
// tests would have to compute the HMAC; the integration path against
// a real OpenSRS endpoint does that upstream).
//
// Construct with NewServer; defer Close() to release the listener.
// The returned *registrar.OpenSRSProvider is configured to point at
// the fake so a test can wire provider + service end-to-end.
package fake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/registrar"
)

// uuidShort returns the first 8 characters of a fresh UUID.
func uuidShort() string {
	return strings.ReplaceAll(strings.ReplaceAll(uuid.New().String(), "-", ""), "_", "")[:8]
}

// Server is an in-memory httptest-backed fake of the OpenSRS reseller
// API. State is keyed by domain name; every mutation runs under the
// embedded mutex so concurrent tests do not race.
type Server struct {
	*httptest.Server

	mu        sync.Mutex
	domains   map[string]*fakeDomain // canonical name (no trailing dot) -> state
	orders    map[string]string      // order_id -> domain
	nextOrder int
	// suffix is a per-server UUID fragment appended to every order id
	// so two fakes sharing one DB (e.g. parallel tests) cannot collide
	// on the cross-tenant unique index uq_dns_domains_order_id.
	suffix string
	// FailOn maps an HTTP method + path suffix to an error the server
	// returns. Used by tests to drive the error path.
	FailOn map[string]error
	// DefaultPriceCents is the per-year price the fake reports for
	// every Check call. Default 1000 (= $10.00).
	DefaultPriceCents int64
	// DefaultCurrency is the currency the fake reports. Default "USD".
	DefaultCurrency string
}

type fakeDomain struct {
	domain     registrar.GetDomainResponse
	registered bool
	authCode   string
	dsRecords  []registrar.DSRecord
	createdAt  time.Time
}

// NewServer builds a fake registrar HTTP server. The returned Server is
// live; the test must Close() it.
func NewServer() *Server {
	s := &Server{
		domains:           make(map[string]*fakeDomain),
		orders:            make(map[string]string),
		DefaultPriceCents: 1000,
		DefaultCurrency:   "USD",
		suffix:            shortUUID(),
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handler))
	return s
}

// shortUUID returns the first 8 characters of a fresh UUID — enough
// entropy to keep parallel-test order ids unique without bloating the
// audit log.
func shortUUID() string {
	return uuidShort()
}

// Provider builds an OpenSRSProvider pointing at this fake. The
// returned provider can be passed directly to registrar.New.
func (s *Server) Provider() *registrar.OpenSRSProvider {
	p, err := registrar.NewOpenSRSProvider(registrar.OpenSRSConfig{
		HTTPClient: s.Client(),
		BaseURL:    s.URL,
		APIKey:     "fake-test-key",
		Username:   "fake-test-user",
	})
	if err != nil {
		// Should never happen with the required fields populated above.
		panic(fmt.Sprintf("fake registrar: build provider: %v", err))
	}
	return p
}

// Seed inserts a domain into the fake's state. Used by tests that need
// a pre-existing registration (e.g. the renew / transfer / get paths).
func (s *Server) Seed(d registrar.GetDomainResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.domains[d.Domain] = &fakeDomain{
		domain:     d,
		registered: d.Status == "registered" || d.Status == "transferred",
		createdAt:  time.Now().UTC(),
	}
	if d.OrderID != "" {
		s.orders[d.OrderID] = d.Domain
	}
}

// handler routes every /v3/<...> request to the right fake path.
func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v3/")
	method := r.Method
	key := method + " " + path
	if err, fail := s.FailOn[key]; fail {
		writeError(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	switch {
	case method == http.MethodGet && path == "account/usage":
		writeJSON(w, http.StatusOK, map[string]any{"balance_cents": 100000})
	case method == http.MethodPost && path == "domains/lookup":
		s.handleLookup(w, r)
	case method == http.MethodPost && path == "domains":
		s.handleRegister(w, r)
	case method == http.MethodPost && strings.HasSuffix(path, "/renew"):
		s.handleRenew(w, r, path)
	case method == http.MethodPost && strings.HasSuffix(path, "/transfer"):
		s.handleTransfer(w, r, path)
	case method == http.MethodPost && strings.HasSuffix(path, "/dnssec/ds"):
		s.handleSetDS(w, r, path)
	case method == http.MethodGet && strings.HasPrefix(path, "domains/"):
		s.handleGet(w, r, path)
	default:
		writeError(w, http.StatusNotFound, "404", "no such path: "+key)
	}
}

func (s *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Domain string `json:"domain"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "400", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, taken := s.domains[body.Domain]
	resp := map[string]any{
		"status": "available",
		"pricing": []map[string]any{
			{"period_years": 1, "price_cents": s.DefaultPriceCents, "currency": s.DefaultCurrency},
			{"period_years": 2, "price_cents": s.DefaultPriceCents * 2, "currency": s.DefaultCurrency},
			{"period_years": 5, "price_cents": s.DefaultPriceCents * 5, "currency": s.DefaultCurrency},
		},
	}
	if taken && d.registered {
		resp["status"] = "unavailable"
		resp["reason"] = "Domain already registered"
	}
	writeJSON(w, http.StatusOK, resp)
}

// Wire request shapes. The driver sends these JSON shapes (per
// opensrs.go's opensrs*Request structs); the fake decodes them so the
// handler sees the real wire payload. Using the wire shape (not the
// registrar.Register*Request domain type) keeps the fake end-to-end
// faithful — a typo in the driver's JSON tag fails the test the same
// way a real OpenSRS would.
type wireContact struct {
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Address1  string `json:"address1"`
	City      string `json:"city"`
	State     string `json:"state"`
	Zip       string `json:"zip"`
	Country   string `json:"country"`
}

type wireRegisterRequest struct {
	Domain       string      `json:"domain"`
	PeriodYears  int32       `json:"period_years"`
	AutoRenew    bool        `json:"auto_renew"`
	WHOISPrivacy bool        `json:"whois_privacy"`
	Owner        wireContact `json:"owner"`
}

type wireRenewRequest struct {
	OrderID     string `json:"order_id"`
	PeriodYears int32  `json:"period_years"`
}

type wireTransferRequest struct {
	Domain      string      `json:"domain"`
	AuthCode    string      `json:"auth_code"`
	PeriodYears int32       `json:"period_years"`
	Owner       wireContact `json:"owner"`
}

type wireSetDSRequest struct {
	Records []struct {
		KeyTag     int32  `json:"key_tag"`
		Algorithm  int32  `json:"algorithm"`
		DigestType int32  `json:"digest_type"`
		Digest     string `json:"digest"`
	} `json:"records"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body wireRegisterRequest
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "400", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, exists := s.domains[body.Domain]; exists && d.registered {
		writeError(w, http.StatusConflict, "409", "Domain already exists")
		return
	}
	s.nextOrder++
	// Include a per-server UUID suffix so two fakes sharing one DB
	// (e.g. parallel tests in the same test package) cannot collide on
	// the cross-tenant unique index uq_dns_domains_order_id.
	orderID := fmt.Sprintf("ORD-%d-%s", s.nextOrder, s.suffix)
	years := int(body.PeriodYears)
	if years <= 0 {
		years = 1
	}
	expires := time.Now().UTC().AddDate(years, 0, 0)
	s.domains[body.Domain] = &fakeDomain{
		domain: registrar.GetDomainResponse{
			Domain:      body.Domain,
			OrderID:     orderID,
			Status:      "registered",
			ExpiresAt:   &expires,
			AutoRenew:   body.AutoRenew,
			PrivacyOn:   body.WHOISPrivacy,
			Nameservers: []string{},
		},
		registered: true,
		createdAt:  time.Now().UTC(),
	}
	s.orders[orderID] = body.Domain
	writeJSON(w, http.StatusOK, map[string]any{
		"order_id":    orderID,
		"domain":      body.Domain,
		"status":      "registered",
		"expires_at":  expires,
		"price_cents": s.DefaultPriceCents * int64(years),
		"currency":    s.DefaultCurrency,
	})
}

func (s *Server) handleRenew(w http.ResponseWriter, r *http.Request, path string) {
	domain := strings.TrimSuffix(strings.TrimSuffix(path, "/renew"), "")
	domain = strings.TrimPrefix(domain, "domains/")
	var body wireRenewRequest
	_ = decodeBody(r, &body)
	years := int(body.PeriodYears)
	if years <= 0 {
		years = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, exists := s.domains[domain]
	if !exists || !d.registered {
		writeError(w, http.StatusNotFound, "404", "Domain not registered")
		return
	}
	// Extend expiry.
	exp := time.Now().UTC().AddDate(years, 0, 0)
	if d.domain.ExpiresAt != nil && d.domain.ExpiresAt.After(time.Now()) {
		exp = d.domain.ExpiresAt.AddDate(years, 0, 0)
	}
	d.domain.ExpiresAt = &exp
	writeJSON(w, http.StatusOK, map[string]any{
		"order_id":    d.domain.OrderID,
		"domain":      domain,
		"status":      "registered",
		"expires_at":  exp,
		"price_cents": s.DefaultPriceCents * int64(years),
		"currency":    s.DefaultCurrency,
	})
}

func (s *Server) handleTransfer(w http.ResponseWriter, r *http.Request, path string) {
	domain := strings.TrimSuffix(strings.TrimSuffix(path, "/transfer"), "")
	domain = strings.TrimPrefix(domain, "domains/")
	var body wireTransferRequest
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "400", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, exists := s.domains[domain]; exists && d.registered {
		// Existing registration with a different auth code = locked.
		if d.authCode != "" && d.authCode != body.AuthCode {
			writeError(w, http.StatusConflict, "409", "Domain locked for transfer")
			return
		}
	}
	s.nextOrder++
	orderID := fmt.Sprintf("XFR-%d-%s", s.nextOrder, s.suffix)
	years := int(body.PeriodYears)
	if years <= 0 {
		years = 1
	}
	expires := time.Now().UTC().AddDate(years, 0, 0)
	s.domains[domain] = &fakeDomain{
		domain: registrar.GetDomainResponse{
			Domain:    domain,
			OrderID:   orderID,
			Status:    "transferred",
			ExpiresAt: &expires,
		},
		registered: true,
		createdAt:  time.Now().UTC(),
	}
	s.orders[orderID] = domain
	writeJSON(w, http.StatusOK, map[string]any{
		"order_id":    orderID,
		"domain":      domain,
		"status":      "transferred",
		"expires_at":  expires,
		"price_cents": s.DefaultPriceCents * int64(years),
		"currency":    s.DefaultCurrency,
	})
}

func (s *Server) handleGet(w http.ResponseWriter, _ *http.Request, path string) {
	domain := strings.TrimPrefix(path, "domains/")
	s.mu.Lock()
	defer s.mu.Unlock()
	d, exists := s.domains[domain]
	if !exists {
		writeError(w, http.StatusNotFound, "404", "Domain not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain":      d.domain.Domain,
		"order_id":    d.domain.OrderID,
		"status":      d.domain.Status,
		"expires_at":  d.domain.ExpiresAt,
		"auto_renew":  d.domain.AutoRenew,
		"locked":      d.domain.Locked,
		"privacy_on":  d.domain.PrivacyOn,
		"nameservers": d.domain.Nameservers,
	})
}

func (s *Server) handleSetDS(w http.ResponseWriter, r *http.Request, path string) {
	domain := strings.TrimSuffix(strings.TrimSuffix(path, "/dnssec/ds"), "")
	domain = strings.TrimPrefix(domain, "domains/")
	var body wireSetDSRequest
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "400", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, exists := s.domains[domain]
	if !exists || !d.registered {
		writeError(w, http.StatusNotFound, "404", "Domain not registered")
		return
	}
	ds := make([]registrar.DSRecord, 0, len(body.Records))
	for _, r := range body.Records {
		ds = append(ds, registrar.DSRecord{
			KeyTag:     r.KeyTag,
			Algorithm:  r.Algorithm,
			DigestType: r.DigestType,
			Digest:     r.Digest,
		})
	}
	d.dsRecords = ds
	writeJSON(w, http.StatusOK, map[string]any{
		"applied":      true,
		"record_count": int32(len(body.Records)),
	})
}

// decodeBody reads the request body into out.
func decodeBody(r *http.Request, out any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(out)
}

// writeJSON marshals body + writes it with the given status.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError writes an OpenSRS-shaped error envelope.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"response": map[string]any{
			"code": code,
			"text": msg,
		},
	})
}

// Ping verifies the fake is reachable. Convenience for tests.
func (s *Server) Ping(ctx context.Context) error {
	p := s.Provider()
	return p.Ping(ctx)
}
