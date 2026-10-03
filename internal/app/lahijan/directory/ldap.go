package directory

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// ldapConn is the slice of an LDAP connection the directory service uses. It
// exists so tests can substitute a fake server.
type ldapConn interface {
	// Search runs a subtree search. sizeLimit 0 pages through and returns every
	// entry; a positive sizeLimit returns at most that many (not an error when
	// the server holds more).
	Search(baseDN, filter string, attrs []string, sizeLimit int) ([]ldapEntry, error)
	// Bind authenticates the connection as dn. An empty password is rejected
	// (LDAP would treat it as an unauthenticated bind and report success).
	Bind(dn, password string) error
	Close()
}

// ldapEntry is one search result.
type ldapEntry struct {
	DN    string
	Attrs map[string][]string // keyed by lower-case attribute name
}

// first returns the first value of attr (case-insensitive), or "".
func (e ldapEntry) first(attr string) string {
	if v := e.Attrs[strings.ToLower(attr)]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// all returns every value of attr (case-insensitive).
func (e ldapEntry) all(attr string) []string { return e.Attrs[strings.ToLower(attr)] }

// ldapDialer opens a bound connection. It is a field on Service so tests can
// inject a fake.
type ldapDialer func(cfg LDAPConfig, bindPassword string) (ldapConn, error)

// Timeouts so a slow or unreachable directory cannot hang a sign-in.
const (
	ldapDialTimeout = 10 * time.Second
	ldapOpTimeout   = 15 * time.Second
)

// ErrLDAPConnect wraps a failure to reach or bind to the server.
var ErrLDAPConnect = errors.New("directory: ldap connect failed")

// dialLDAP connects, optionally upgrades with STARTTLS, and binds.
func dialLDAP(cfg LDAPConfig, bindPassword string) (ldapConn, error) {
	tlsCfg := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec // opt-in per connection, for lab servers
	conn, err := ldap.DialURL(cfg.URL,
		ldap.DialWithDialer(&net.Dialer{Timeout: ldapDialTimeout}),
		ldap.DialWithTLSConfig(tlsCfg))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLDAPConnect, err)
	}
	conn.SetTimeout(ldapOpTimeout)
	if cfg.StartTLS {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, fmt.Errorf("%w: starttls: %w", ErrLDAPConnect, err)
		}
	}
	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, bindPassword); err != nil {
			conn.Close()
			return nil, fmt.Errorf("%w: bind: %w", ErrLDAPConnect, err)
		}
	} else if err := conn.UnauthenticatedBind(""); err != nil {
		conn.Close()
		return nil, fmt.Errorf("%w: anonymous bind: %w", ErrLDAPConnect, err)
	}
	return &realConn{c: conn}, nil
}

type realConn struct{ c *ldap.Conn }

func (r *realConn) Close() { r.c.Close() }

func (r *realConn) Bind(dn, password string) error {
	if password == "" {
		return errors.New("ldap: empty password")
	}
	if err := r.c.Bind(dn, password); err != nil {
		return fmt.Errorf("ldap bind: %w", err)
	}
	return nil
}

// escapeFilter escapes a value for safe use inside an LDAP search filter.
func escapeFilter(v string) string { return ldap.EscapeFilter(v) }

func (r *realConn) Search(baseDN, filter string, attrs []string, sizeLimit int) ([]ldapEntry, error) {
	req := ldap.NewSearchRequest(baseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, sizeLimit, 0, false, filter, attrs, nil)
	var res *ldap.SearchResult
	var err error
	if sizeLimit > 0 {
		res, err = r.c.Search(req)
		if err != nil && ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) && res != nil {
			err = nil // we asked for a sample; more entries existing is fine
		}
	} else {
		res, err = r.c.SearchWithPaging(req, 500)
	}
	if err != nil {
		return nil, fmt.Errorf("ldap search %q: %w", baseDN, err)
	}
	out := make([]ldapEntry, 0, len(res.Entries))
	for _, e := range res.Entries {
		m := make(map[string][]string, len(e.Attributes))
		for _, a := range e.Attributes {
			m[strings.ToLower(a.Name)] = a.Values
		}
		out = append(out, ldapEntry{DN: e.DN, Attrs: m})
	}
	return out, nil
}
