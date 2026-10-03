package directory

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

type fakeConn struct {
	passwords     map[string]string // DN -> password accepted by Bind
	users, groups []ldapEntry
	searchErr     error
}

func (f *fakeConn) Close() {}

// Bind succeeds only for a DN listed in passwords with the matching password.
func (f *fakeConn) Bind(dn, password string) error {
	if password != "" && f.passwords[dn] == password {
		return nil
	}
	return errors.New("invalid credentials")
}

func (f *fakeConn) Search(base, _ string, _ []string, limit int) ([]ldapEntry, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	list := f.users
	if base == "ou=groups,dc=x" {
		list = f.groups
	}
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func ldapCfgNoDefaults(t *testing.T) LDAPConfig {
	t.Helper()
	var c LDAPConfig
	require.NoError(t, json.Unmarshal(ldapCfg(t, nil), &c))
	return c.withDefaults()
}

func ldapCfg(t *testing.T, mutate func(*LDAPConfig)) json.RawMessage {
	t.Helper()
	c := LDAPConfig{URL: "ldap://dir.example.com", UserBaseDN: "ou=people,dc=x", GroupBaseDN: "ou=groups,dc=x"}
	if mutate != nil {
		mutate(&c)
	}
	b, err := json.Marshal(c)
	require.NoError(t, err)
	return b
}

func TestLDAPConfigValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*LDAPConfig)
		wantErr bool
	}{
		{"valid", nil, false},
		{"bad scheme", func(c *LDAPConfig) { c.URL = "http://x" }, true},
		{"missing host", func(c *LDAPConfig) { c.URL = "ldap://" }, true},
		{"no user base", func(c *LDAPConfig) { c.UserBaseDN = "" }, true},
		{"starttls with ldaps", func(c *LDAPConfig) { c.URL = "ldaps://x"; c.StartTLS = true }, true},
		{"ldaps ok", func(c *LDAPConfig) { c.URL = "ldaps://x" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := normalizeConfig(KindLDAP, ldapCfg(t, tc.mutate))
			if tc.wantErr {
				require.ErrorIs(t, err, ErrInvalid)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLDAPConfigDefaults(t *testing.T) {
	t.Parallel()
	raw, err := normalizeConfig(KindLDAP, ldapCfg(t, nil))
	require.NoError(t, err)
	var c LDAPConfig
	require.NoError(t, json.Unmarshal(raw, &c))
	require.Equal(t, "mail", c.EmailAttr)
	require.Equal(t, "(objectClass=person)", c.UserFilter)
	require.Equal(t, "member", c.GroupMemberAttr)
}

func TestSAMLConfigValidate(t *testing.T) {
	t.Parallel()
	_, err := normalizeConfig(KindSAML, json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrInvalid)
	_, err = normalizeConfig(KindSAML, json.RawMessage(`{"idpMetadataUrl":"ftp://x"}`))
	require.ErrorIs(t, err, ErrInvalid)
	_, err = normalizeConfig(KindSAML, json.RawMessage(`{"idpMetadataUrl":"https://idp.example.com/metadata"}`))
	require.NoError(t, err)
	_, err = normalizeConfig("radius", json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrInvalid)
}

func TestValidateName(t *testing.T) {
	t.Parallel()
	require.NoError(t, ValidateName("corp-ldap_1"))
	for _, bad := range []string{"", "Corp", "-x", "a b", "a/b"} {
		require.ErrorIs(t, ValidateName(bad), ErrInvalid, bad)
	}
}

func TestNormDN(t *testing.T) {
	t.Parallel()
	require.Equal(t, "cn=ann,ou=people,dc=x", normDN(" CN=Ann, OU=People ,DC=X "))
}

func TestServiceTestLDAP(t *testing.T) {
	t.Parallel()
	cfg, err := normalizeConfig(KindLDAP, ldapCfg(t, nil))
	require.NoError(t, err)

	t.Run("ok", func(t *testing.T) {
		t.Parallel()
		fc := &fakeConn{
			users:  []ldapEntry{{DN: "uid=a"}, {DN: "uid=b"}},
			groups: []ldapEntry{{DN: "cn=g"}},
		}
		s := &Service{dial: func(LDAPConfig, string) (ldapConn, error) { return fc, nil }}
		res := s.testLDAP(cfg, "pw")
		require.True(t, res.OK)
		require.Equal(t, 2, res.Users)
		require.Equal(t, 1, res.Groups)
	})
	t.Run("connect failure", func(t *testing.T) {
		t.Parallel()
		s := &Service{dial: func(LDAPConfig, string) (ldapConn, error) { return nil, ErrLDAPConnect }}
		res := s.testLDAP(cfg, "pw")
		require.False(t, res.OK)
		require.Equal(t, "connect_failed", res.Code)
	})
	t.Run("search failure", func(t *testing.T) {
		t.Parallel()
		fc := &fakeConn{searchErr: errors.New("no such object")}
		s := &Service{dial: func(LDAPConfig, string) (ldapConn, error) { return fc, nil }}
		res := s.testLDAP(cfg, "pw")
		require.False(t, res.OK)
		require.Equal(t, "search_failed", res.Code)
	})
}

func TestEntryAttrs(t *testing.T) {
	t.Parallel()
	e := ldapEntry{Attrs: map[string][]string{"mail": {"a@x"}, "member": {"d1", "d2"}}}
	require.Equal(t, "a@x", e.first("MAIL"))
	require.Equal(t, []string{"d1", "d2"}, e.all("Member"))
	require.Empty(t, e.first("missing"))
}

type fakeActivator struct {
	active  map[string]bool
	failFor string
}

func newFakeActivator() *fakeActivator { return &fakeActivator{active: map[string]bool{}} }

func (f *fakeActivator) Activate(name string, _ SAMLConfig) error {
	if name == f.failFor {
		return errors.New("metadata unreachable")
	}
	f.active[name] = true
	return nil
}
func (f *fakeActivator) Deactivate(name string)    { delete(f.active, name) }
func (f *fakeActivator) IsActive(name string) bool { return f.active[name] }

func samlRow(name string, enabled bool) gen.DirectoryConnection {
	return gen.DirectoryConnection{
		Kind: KindSAML, Name: name, Enabled: enabled,
		Config: json.RawMessage(`{"idpMetadataUrl":"https://idp.example.com/m"}`),
	}
}

func TestApplySAMLChange(t *testing.T) {
	t.Parallel()
	fa := newFakeActivator()
	s := &Service{saml: fa}

	// Create enabled -> active.
	row := samlRow("sso", true)
	require.NoError(t, s.applySAMLChange(nil, &row))
	require.True(t, fa.active["sso"])

	// Rename -> old key removed, new key active.
	renamed := samlRow("sso2", true)
	require.NoError(t, s.applySAMLChange(&row, &renamed))
	require.False(t, fa.active["sso"])
	require.True(t, fa.active["sso2"])

	// Disable -> deactivated.
	disabled := samlRow("sso2", false)
	require.NoError(t, s.applySAMLChange(&renamed, &disabled))
	require.False(t, fa.active["sso2"])

	// Delete -> deactivated.
	require.NoError(t, s.applySAMLChange(&renamed, &renamed)) // re-enable via same row
	require.True(t, fa.active["sso2"])
	require.NoError(t, s.applySAMLChange(&renamed, nil))
	require.False(t, fa.active["sso2"])

	// An activation failure is returned, and the connection is not active.
	fa.failFor = "bad"
	bad := samlRow("bad", true)
	require.Error(t, s.applySAMLChange(nil, &bad))
	require.False(t, fa.active["bad"])

	// LDAP rows never touch the registry.
	ldap := gen.DirectoryConnection{Kind: KindLDAP, Name: "corp", Enabled: true}
	require.NoError(t, s.applySAMLChange(nil, &ldap))
	require.False(t, fa.active["corp"])
}

func TestDecorateActive(t *testing.T) {
	t.Parallel()
	fa := newFakeActivator()
	fa.active["sso"] = true
	s := &Service{saml: fa}

	require.True(t, s.decorate(Connection{Kind: KindSAML, Name: "sso", Enabled: true}).Active)
	require.False(t, s.decorate(Connection{Kind: KindSAML, Name: "other", Enabled: true}).Active)
	require.False(t, s.decorate(Connection{Kind: KindSAML, Name: "sso", Enabled: false}).Active)
	require.True(t, s.decorate(Connection{Kind: KindLDAP, Name: "corp", Enabled: true}).Active)
	require.False(t, s.decorate(Connection{Kind: KindLDAP, Name: "corp", Enabled: false}).Active)
}

func TestStringValues(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"a"}, stringValues("a"))
	require.Equal(t, []string{"a", "b"}, stringValues([]string{" a ", "", "b"}))
	require.Equal(t, []string{"a"}, stringValues([]any{"a", 3, ""}))
	require.Empty(t, stringValues(nil))
}
