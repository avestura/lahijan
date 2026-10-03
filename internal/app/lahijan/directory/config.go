// Package directory manages connections to external user directories: LDAP
// servers that Lahijan can sync users and groups from, and SAML identity
// providers that sign users in and assert their groups.
//
// Connections are platform-level (a platform administrator defines them); the
// users and groups they import live in the global users / directory_* tables.
package directory

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Connection kinds.
const (
	// KindLDAP is an LDAP / Active Directory server (user + group sync).
	KindLDAP = "ldap"
	// KindSAML is a SAML 2.0 identity provider (sign-in + group assertions).
	KindSAML = "saml"
)

// Validation errors. They wrap ErrInvalid so callers can map them to 400.
var (
	// ErrInvalid marks a connection definition the admin must fix.
	ErrInvalid = errors.New("directory: invalid connection")
	// ErrNotFound is returned for an unknown connection id.
	ErrNotFound = errors.New("directory: connection not found")
	// ErrNameTaken is returned when a connection name is already in use.
	ErrNameTaken = errors.New("directory: connection name already in use")
	// ErrCryptoRequired is returned when a secret must be stored but no
	// encryption key is configured.
	ErrCryptoRequired = errors.New("directory: encryption key not configured")
	// ErrSyncFailed wraps a failure while talking to the directory during sync.
	ErrSyncFailed = errors.New("directory: sync failed")
	// ErrSyncUnsupported is returned when Sync is called on a kind with no sync.
	ErrSyncUnsupported = errors.New("directory: this connection kind does not support sync")
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// LDAPConfig is the non-secret LDAP definition. The bind password is stored
// separately (sealed) and never appears here.
type LDAPConfig struct {
	// URL is ldap://host[:389] or ldaps://host[:636].
	URL string `json:"url"`
	// StartTLS upgrades a plain ldap:// connection with STARTTLS.
	StartTLS bool `json:"startTls"`
	// InsecureSkipVerify disables TLS certificate verification. Test labs only.
	InsecureSkipVerify bool `json:"insecureSkipVerify"`
	// BindDN is the service account used to search. Empty binds anonymously.
	BindDN string `json:"bindDn"`

	// UserBaseDN is where users are searched (subtree).
	UserBaseDN string `json:"userBaseDn"`
	// UserFilter selects user entries. Default (objectClass=person).
	UserFilter string `json:"userFilter"`
	// EmailAttr holds the user's email. Default mail.
	EmailAttr string `json:"emailAttr"`
	// NameAttr holds the display name. Default displayName (falls back to cn).
	NameAttr string `json:"nameAttr"`

	// GroupBaseDN is where groups are searched; empty skips group sync.
	GroupBaseDN string `json:"groupBaseDn"`
	// GroupFilter selects group entries. Default (objectClass=groupOfNames).
	GroupFilter string `json:"groupFilter"`
	// GroupNameAttr holds the group name. Default cn.
	GroupNameAttr string `json:"groupNameAttr"`
	// GroupMemberAttr lists member DNs. Default member.
	GroupMemberAttr string `json:"groupMemberAttr"`

	// CreateUsersOnLogin makes the first successful sign-in create the Lahijan
	// account for a directory user who has not been synced yet. Default true.
	// With false, only users that already exist (imported by a sync, or
	// created by an administrator) can sign in with the directory password.
	CreateUsersOnLogin *bool `json:"createUsersOnLogin,omitempty"`
}

// withDefaults fills empty optional fields.
func (c LDAPConfig) withDefaults() LDAPConfig {
	set := func(p *string, def string) {
		if strings.TrimSpace(*p) == "" {
			*p = def
		}
	}
	set(&c.UserFilter, "(objectClass=person)")
	set(&c.EmailAttr, "mail")
	set(&c.NameAttr, "displayName")
	set(&c.GroupFilter, "(objectClass=groupOfNames)")
	set(&c.GroupNameAttr, "cn")
	set(&c.GroupMemberAttr, "member")
	if c.CreateUsersOnLogin == nil {
		on := true
		c.CreateUsersOnLogin = &on
	}
	return c
}

// createOnLogin reports whether sign-in may create a missing account.
func (c LDAPConfig) createOnLogin() bool { return c.CreateUsersOnLogin == nil || *c.CreateUsersOnLogin }

// Validate checks the definition and returns it with defaults applied.
func (c LDAPConfig) Validate() (LDAPConfig, error) {
	c = c.withDefaults()
	u, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil || (u.Scheme != "ldap" && u.Scheme != "ldaps") || u.Host == "" {
		return c, fmt.Errorf("%w: url must be ldap://host or ldaps://host", ErrInvalid)
	}
	if c.StartTLS && u.Scheme == "ldaps" {
		return c, fmt.Errorf("%w: startTls cannot be combined with ldaps://", ErrInvalid)
	}
	if strings.TrimSpace(c.UserBaseDN) == "" {
		return c, fmt.Errorf("%w: userBaseDn is required", ErrInvalid)
	}
	return c, nil
}

// SAMLConfig is the SAML identity-provider definition.
type SAMLConfig struct {
	// IDPMetadataURL is where the IdP publishes its metadata.
	IDPMetadataURL string `json:"idpMetadataUrl"`
	// IDPMetadataXML is inline IdP metadata; wins over the URL when set.
	IDPMetadataXML string `json:"idpMetadataXml"`
	// EntityID overrides this service provider's entity id (optional).
	EntityID string `json:"entityId"`
	// EmailAttribute / NameAttribute / GroupsAttribute name the SAML
	// attributes that carry the user's email, display name and groups.
	EmailAttribute  string `json:"emailAttribute"`
	NameAttribute   string `json:"nameAttribute"`
	GroupsAttribute string `json:"groupsAttribute"`
	// AllowIDPInitiated accepts IdP-initiated responses (off by default).
	AllowIDPInitiated bool `json:"allowIdpInitiated"`
}

// Validate checks the definition.
func (c SAMLConfig) Validate() (SAMLConfig, error) {
	if strings.TrimSpace(c.IDPMetadataXML) == "" && strings.TrimSpace(c.IDPMetadataURL) == "" {
		return c, fmt.Errorf("%w: provide idpMetadataUrl or idpMetadataXml", ErrInvalid)
	}
	if u := strings.TrimSpace(c.IDPMetadataURL); u != "" {
		p, err := url.Parse(u)
		if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" {
			return c, fmt.Errorf("%w: idpMetadataUrl must be an http(s) URL", ErrInvalid)
		}
	}
	return c, nil
}

// ValidateName checks a connection name (also the SAML provider key).
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("%w: name must be lowercase letters, digits, '-' or '_' (max 63)", ErrInvalid)
	}
	return nil
}

// normalizeConfig validates raw config for a kind and returns canonical JSON.
func normalizeConfig(kind string, raw json.RawMessage) (json.RawMessage, error) {
	switch kind {
	case KindLDAP:
		var c LDAPConfig
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("%w: config: %w", ErrInvalid, err)
		}
		c, err := c.Validate()
		if err != nil {
			return nil, err
		}
		return json.Marshal(c)
	case KindSAML:
		var c SAMLConfig
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("%w: config: %w", ErrInvalid, err)
		}
		c, err := c.Validate()
		if err != nil {
			return nil, err
		}
		return json.Marshal(c)
	default:
		return nil, fmt.Errorf("%w: unknown kind %q", ErrInvalid, kind)
	}
}
