package directory

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loginPerson(uid, mail, name string) ldapEntry {
	return ldapEntry{
		DN:    "uid=" + uid + ",ou=people,dc=x",
		Attrs: map[string][]string{"mail": {mail}, "displayname": {name}},
	}
}

func loginService(conn ldapConn) *Service {
	return &Service{
		log:  slog.Default(),
		dial: func(LDAPConfig, string) (ldapConn, error) { return conn, nil },
	}
}

func TestVerifyLDAPCredentials(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := ldapCfgNoDefaults(t)
	ann := loginPerson("ann", "ann@example.test", "Ann A")

	t.Run("right password", func(t *testing.T) {
		t.Parallel()
		fc := &fakeConn{users: []ldapEntry{ann}, passwords: map[string]string{ann.DN: "pw"}}
		entry, ok := loginService(fc).verifyLDAPCredentials(ctx, "corp", cfg, "svc", "ann@example.test", "pw")
		require.True(t, ok)
		assert.Equal(t, ann.DN, entry.DN)
	})
	t.Run("wrong password", func(t *testing.T) {
		t.Parallel()
		fc := &fakeConn{users: []ldapEntry{ann}, passwords: map[string]string{ann.DN: "pw"}}
		_, ok := loginService(fc).verifyLDAPCredentials(ctx, "corp", cfg, "svc", "ann@example.test", "nope")
		assert.False(t, ok)
	})
	t.Run("unknown user", func(t *testing.T) {
		t.Parallel()
		_, ok := loginService(&fakeConn{}).verifyLDAPCredentials(ctx, "corp", cfg, "svc", "x@example.test", "pw")
		assert.False(t, ok)
	})
	t.Run("ambiguous match is refused", func(t *testing.T) {
		t.Parallel()
		other := loginPerson("ann2", "ann@example.test", "Ann Two")
		fc := &fakeConn{
			users:     []ldapEntry{ann, other},
			passwords: map[string]string{ann.DN: "pw", other.DN: "pw"},
		}
		_, ok := loginService(fc).verifyLDAPCredentials(ctx, "corp", cfg, "svc", "ann@example.test", "pw")
		assert.False(t, ok, "two entries share the email, so no one is signed in")
	})
	t.Run("directory down", func(t *testing.T) {
		t.Parallel()
		s := &Service{log: slog.Default(), dial: func(LDAPConfig, string) (ldapConn, error) { return nil, ErrLDAPConnect }}
		_, ok := s.verifyLDAPCredentials(ctx, "corp", cfg, "svc", "ann@example.test", "pw")
		assert.False(t, ok)
	})
	t.Run("search failure", func(t *testing.T) {
		t.Parallel()
		_, ok := loginService(&fakeConn{searchErr: assert.AnError}).verifyLDAPCredentials(ctx, "corp", cfg, "svc", "a@example.test", "pw")
		assert.False(t, ok)
	})
}

func TestAuthenticateExternalRejectsBeforeAnyLookup(t *testing.T) {
	t.Parallel()
	// A nil repository proves these inputs never reach the directory: an empty
	// password would otherwise be an unauthenticated LDAP bind that "succeeds".
	s := &Service{log: slog.Default()}
	ctx := context.Background()
	for _, tc := range []struct{ email, password string }{
		{"ann@example.test", ""},
		{"  ", "pw"},
		{"", ""},
		{"ann@example.test", strings.Repeat("x", maxPasswordLen+1)},
	} {
		uid, ok := s.AuthenticateExternal(ctx, tc.email, tc.password)
		assert.False(t, ok)
		assert.Equal(t, uuid.Nil, uid)
	}
}

func TestUserLookupFilter(t *testing.T) {
	t.Parallel()
	cfg := ldapCfgNoDefaults(t)
	assert.Equal(t, "(&(objectClass=person)(mail=ann@example.test))", userLookupFilter(cfg, "ann@example.test"))

	// A user filter written without parentheses is wrapped.
	cfg.UserFilter = "objectClass=inetOrgPerson"
	assert.Equal(t, "(&(objectClass=inetOrgPerson)(mail=a@b))", userLookupFilter(cfg, "a@b"))

	// Filter metacharacters in the email cannot change the query.
	got := userLookupFilter(cfg, "x*)(uid=*")
	assert.NotContains(t, got, "x*)(uid=*")
	assert.Contains(t, got, `\2a`)
	assert.Contains(t, got, `\28`)
	assert.Contains(t, got, `\29`)
}

func TestCreateUsersOnLoginDefault(t *testing.T) {
	t.Parallel()
	assert.True(t, LDAPConfig{}.createOnLogin())
	assert.True(t, LDAPConfig{}.withDefaults().createOnLogin())
	off := false
	assert.False(t, LDAPConfig{CreateUsersOnLogin: &off}.withDefaults().createOnLogin())
}
