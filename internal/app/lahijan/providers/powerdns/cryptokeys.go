// Package powerdns: cryptokeys.go wraps the PDNS DNSSEC key-management API.
// Each zone has zero or more crypto keys (a key-signing key + a zone-signing
// key when DNSSEC is enabled); the driver surfaces enable / disable / list /
// get / delete plus the per-zone "is DNSSEC on?" toggle.
//
// PDNS' DNSSEC model: enabling DNSSEC for a zone = create one KSK + one ZSK
// (or one CSK for combined-signing keys) and mark them active+published.
// Disabling = remove every cryptokey. The driver exposes both as single
// calls; the WS-12 default is "DNSSEC off for new zones" (per the doc's
// "Open questions" item).
package powerdns

import (
	"context"
	"fmt"
)

// CryptoKeyParams carries the user-visible knobs for a new DNSSEC key.
type CryptoKeyParams struct {
	// ZoneID is the canonical zone id.
	ZoneID string

	// KeyType is "ksk" (key-signing), "zsk" (zone-signing), or "csk"
	// (combined). Defaults to "ksk" when enabling DNSSEC via EnableDNSSEC.
	KeyType string

	// Bits is the key length in bits. Defaults to algorithm-appropriate
	// (2048 for KSK/ZSK RSA, 256 for ECDSA).
	Bits int

	// Algorithm is the DNSSEC algorithm. Defaults to "ecdsaP256SHA256"
	// when empty (algorithm 13).
	Algorithm string

	// Active flags whether the key is active immediately.
	Active bool

	// Published flags whether the DNSKEY is published immediately.
	Published bool
}

// CreateCryptoKey creates a DNSSEC signing key for the zone. Returns the
// freshly-created CryptoKey; the Content field (private key material) is
// SENSITIVE — never log.
func (p *Provider) CreateCryptoKey(ctx context.Context, params CryptoKeyParams) (*CryptoKey, error) {
	ctx, span := startSpan(ctx, "cryptokey.create", zoneAttr(params.ZoneID))
	defer span.End()

	if params.KeyType == "" {
		params.KeyType = "ksk"
	}
	body := CryptoKeyCreate{
		KeyType:   params.KeyType,
		Bits:      params.Bits,
		Algorithm: params.Algorithm,
		Active:    params.Active,
		Published: params.Published,
	}
	var created CryptoKey
	if err := p.do(ctx, "POST", cryptokeysPath(params.ZoneID), body, &created); err != nil {
		setStatus(span, err)
		return nil, err
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.zone.dnssec.key_created",
		ActorType:  "system",
		ResourceID: strPtr(params.ZoneID),
		Metadata: asRawJSON(map[string]any{
			"key_id":   created.ID,
			"key_type": created.KeyType,
			"bits":     created.Bits,
		}),
	})
	setStatus(span, nil)
	return &created, nil
}

// ListCryptoKeys returns every DNSSEC key for the zone.
func (p *Provider) ListCryptoKeys(ctx context.Context, zoneID string) ([]CryptoKey, error) {
	ctx, span := startSpan(ctx, "cryptokey.list", zoneAttr(zoneID))
	defer span.End()

	var keys []CryptoKey
	if err := p.do(ctx, "GET", cryptokeysPath(zoneID), nil, &keys); err != nil {
		setStatus(span, err)
		return nil, err
	}
	setStatus(span, nil)
	return keys, nil
}

// GetCryptoKey fetches a single cryptokey by id. The withContent flag
// controls whether PDNS includes the private-key material in the response.
// SENSITIVE — never log the returned Content.
func (p *Provider) GetCryptoKey(ctx context.Context, zoneID string, keyID int64, withContent bool) (*CryptoKey, error) {
	ctx, span := startSpan(ctx, "cryptokey.get", zoneAttr(zoneID))
	defer span.End()

	path := fmt.Sprintf("%s/%d", cryptokeysPath(zoneID), keyID)
	if withContent {
		path += "?non-www-enc=false"
	}
	var key CryptoKey
	if err := p.do(ctx, "GET", path, nil, &key); err != nil {
		setStatus(span, err)
		return nil, err
	}
	setStatus(span, nil)
	return &key, nil
}

// ToggleCryptoKey activates or deactivates a single cryptokey. When a key is
// deactivated PDNS keeps the public material in the zone but stops signing
// with it; useful for key rollover.
func (p *Provider) ToggleCryptoKey(ctx context.Context, zoneID string, keyID int64, active bool) error {
	ctx, span := startSpan(ctx, "cryptokey.toggle", zoneAttr(zoneID))
	defer span.End()

	path := fmt.Sprintf("%s/%d", cryptokeysPath(zoneID), keyID)
	body := CryptoKey{ID: keyID, Active: active}
	if err := p.do(ctx, "PUT", path, body, nil); err != nil {
		setStatus(span, err)
		return err
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.zone.dnssec.key_toggled",
		ActorType:  "system",
		ResourceID: strPtr(zoneID),
		Metadata: asRawJSON(map[string]any{
			"key_id": keyID,
			"active": active,
		}),
	})
	setStatus(span, nil)
	return nil
}

// DeleteCryptoKey removes a single cryptokey. The key must be deactivated
// first (PDNS' policy); callers wanting a "force delete" should call
// ToggleCryptoKey(false) then DeleteCryptoKey.
func (p *Provider) DeleteCryptoKey(ctx context.Context, zoneID string, keyID int64) error {
	ctx, span := startSpan(ctx, "cryptokey.delete", zoneAttr(zoneID))
	defer span.End()

	path := fmt.Sprintf("%s/%d", cryptokeysPath(zoneID), keyID)
	if err := p.do(ctx, "DELETE", path, nil, nil); err != nil {
		setStatus(span, err)
		return err
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.zone.dnssec.key_deleted",
		ActorType:  "system",
		ResourceID: strPtr(zoneID),
		Metadata: asRawJSON(map[string]any{
			"key_id": keyID,
		}),
	})
	setStatus(span, nil)
	return nil
}

// EnableDNSSEC turns DNSSEC on for the zone by creating a single combined
// signing key (CSK). This is the canonical "enable DNSSEC" call Lahijan
// exposes at the service layer; callers wanting finer control (separate KSK
// + ZSK, ECDSA vs RSA) call CreateCryptoKey directly.
//
// Per WS-12 "Open questions" item 1 the default for new zones is OFF; the
// admin opts in per zone via the DNS service.
func (p *Provider) EnableDNSSEC(ctx context.Context, zoneID string) (*CryptoKey, error) {
	ctx, span := startSpan(ctx, "dnssec.enable", zoneAttr(zoneID))
	defer span.End()

	// Idempotent: if a key already exists, return the first active one.
	keys, err := p.ListCryptoKeys(ctx, zoneID)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	for i := range keys {
		if keys[i].Active {
			setStatus(span, nil)
			return &keys[i], nil
		}
	}
	created, err := p.CreateCryptoKey(ctx, CryptoKeyParams{
		ZoneID:    zoneID,
		KeyType:   "csk",
		Algorithm: "ecdsaP256SHA256",
		Bits:      256,
		Active:    true,
		Published: true,
	})
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.zone.dnssec.enabled",
		ActorType:  "system",
		ResourceID: strPtr(zoneID),
	})
	setStatus(span, nil)
	return created, nil
}

// DisableDNSSEC turns DNSSEC off for the zone by deleting every cryptokey.
// Idempotent: if no keys exist the call succeeds without side effect.
func (p *Provider) DisableDNSSEC(ctx context.Context, zoneID string) error {
	ctx, span := startSpan(ctx, "dnssec.disable", zoneAttr(zoneID))
	defer span.End()

	keys, err := p.ListCryptoKeys(ctx, zoneID)
	if err != nil {
		setStatus(span, err)
		return err
	}
	var firstErr error
	for _, k := range keys {
		if err := p.DeleteCryptoKey(ctx, zoneID, k.ID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		setStatus(span, firstErr)
		return firstErr
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.zone.dnssec.disabled",
		ActorType:  "system",
		ResourceID: strPtr(zoneID),
	})
	setStatus(span, nil)
	return nil
}

// IsDNSSECEnabled reports whether the zone has at least one active cryptokey.
// A zone with only inactive keys returns false (PDNS will not be signing).
func (p *Provider) IsDNSSECEnabled(ctx context.Context, zoneID string) (bool, error) {
	ctx, span := startSpan(ctx, "dnssec.status", zoneAttr(zoneID))
	defer span.End()

	keys, err := p.ListCryptoKeys(ctx, zoneID)
	if err != nil {
		setStatus(span, err)
		return false, err
	}
	for _, k := range keys {
		if k.Active {
			setStatus(span, nil)
			return true, nil
		}
	}
	setStatus(span, nil)
	return false, nil
}

// cryptokeysPath builds the cryptokeys REST path for a zone.
func cryptokeysPath(zoneID string) string {
	return zonePath(zoneID) + "/cryptokeys"
}
