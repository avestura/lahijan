// service_integration_test.go exercises the directory service against a real
// Postgres: connection CRUD (the bind password is sealed at rest), an LDAP sync
// that creates / links users and imports groups, and the admin user queries
// that read the result. LDAP itself is replaced by a fake dialer so the test
// needs no directory server.

//go:build integration

package directory

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

func newService(t *testing.T, conn ldapConn) *Service {
	t.Helper()
	crypto, err := secrets.NewCrypto([]byte(strings.Repeat("k", 32)))
	require.NoError(t, err)
	svc := New(integrationRepos(), crypto, audit.NoopEmitter{}, nil)
	svc.dial = func(LDAPConfig, string) (ldapConn, error) { return conn, nil }
	return svc
}

func uniqueName() string { return "ldap-" + uuid.NewString()[:8] }

func ldapInput(name string) Input {
	cfg, _ := json.Marshal(LDAPConfig{
		URL: "ldap://dir.example.com", BindDN: "cn=svc,dc=x", UserBaseDN: "ou=people,dc=x", GroupBaseDN: "ou=groups,dc=x",
	})
	pw := "s3cret"
	return Input{Kind: KindLDAP, Name: name, Enabled: true, Config: cfg, BindPassword: &pw}
}

func person(uid, mail, name string) ldapEntry {
	return ldapEntry{
		DN:    "uid=" + uid + ",ou=people,dc=x",
		Attrs: map[string][]string{"mail": {mail}, "displayname": {name}},
	}
}

func localUser(email string) database.CreateUserParams {
	pw := "argon2id$fake$hash"
	return database.CreateUserParams{Email: email, PasswordHash: &pw}
}

func TestConnectionCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, &fakeConn{})

	name := uniqueName()
	created, err := svc.Create(ctx, uuid.Nil, ldapInput(name))
	require.NoError(t, err)
	assert.True(t, created.HasSecret)
	assert.Equal(t, KindLDAP, created.Kind)

	// The stored bytes are ciphertext, never the plaintext password.
	row, err := svc.dir.GetConnection(ctx, created.ID)
	require.NoError(t, err)
	assert.NotContains(t, string(row.SecretEncrypted), "s3cret")
	pw, err := svc.openSecret(row)
	require.NoError(t, err)
	assert.Equal(t, "s3cret", pw)

	// Duplicate name -> ErrNameTaken.
	_, err = svc.Create(ctx, uuid.Nil, ldapInput(name))
	require.ErrorIs(t, err, ErrNameTaken)

	// Invalid config -> ErrInvalid.
	bad := ldapInput(uniqueName())
	bad.Config = json.RawMessage(`{"url":"http://nope"}`)
	_, err = svc.Create(ctx, uuid.Nil, bad)
	require.ErrorIs(t, err, ErrInvalid)

	// Update without a password keeps the stored one.
	upd := ldapInput(name)
	upd.BindPassword = nil
	upd.Enabled = false
	updated, err := svc.Update(ctx, uuid.Nil, created.ID, upd)
	require.NoError(t, err)
	assert.False(t, updated.Enabled)
	assert.True(t, updated.HasSecret, "secret must survive an update that omits it")

	// Test (saved connection) uses the fake server.
	res, err := svc.Test(ctx, uuid.Nil, &created.ID, nil)
	require.NoError(t, err)
	assert.True(t, res.OK)

	require.NoError(t, svc.Delete(ctx, uuid.Nil, created.ID))
	_, err = svc.Get(ctx, created.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSyncImportsUsersAndGroups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := integrationRepos()

	suffix := uuid.NewString()[:8]
	annMail := "ann-" + suffix + "@example.test"
	bobMail := "bob-" + suffix + "@example.test"
	existingMail := "carol-" + suffix + "@example.test"

	// carol already exists locally; the sync must link her, not duplicate her.
	existing, err := repos.Users.Create(ctx, localUser(existingMail))
	require.NoError(t, err)

	fc := &fakeConn{
		users: []ldapEntry{
			person("ann", annMail, "Ann A"),
			person("bob", bobMail, "Bob B"),
			person("carol", existingMail, "Carol C"),
			{DN: "uid=nomail,ou=people,dc=x", Attrs: map[string][]string{"cn": {"No Mail"}}},
		},
		groups: []ldapEntry{{
			DN: "cn=eng,ou=groups,dc=x",
			Attrs: map[string][]string{
				"cn":     {"eng"},
				"member": {"uid=ann,ou=people,dc=x", "UID=Carol, OU=people,dc=x", "uid=ghost,ou=people,dc=x"},
			},
		}},
	}
	svc := newService(t, fc)
	conn, err := svc.Create(ctx, uuid.Nil, ldapInput(uniqueName()))
	require.NoError(t, err)

	res, err := svc.Sync(ctx, uuid.Nil, conn.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, res.Users)
	assert.Equal(t, 2, res.Created, "ann and bob are new")
	assert.Equal(t, 1, res.Linked, "carol existed")
	assert.Equal(t, 1, res.Skipped, "the entry without a mail attribute")
	assert.Equal(t, 1, res.Groups)

	// New users are verified and have no password; carol is untouched.
	ann, err := repos.Users.GetByEmail(ctx, annMail)
	require.NoError(t, err)
	assert.NotNil(t, ann.EmailVerifiedAt)
	assert.Nil(t, ann.PasswordHash)
	require.NotNil(t, ann.DisplayName)
	assert.Equal(t, "Ann A", *ann.DisplayName)
	carol, err := repos.Users.GetByEmail(ctx, existingMail)
	require.NoError(t, err)
	assert.Equal(t, existing.ID, carol.ID)

	// Group membership resolves DNs case/space-insensitively and ignores unknowns.
	groups, total, err := svc.ListGroups(ctx, conn.ID, 10, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.Equal(t, "eng", groups[0].Name)
	assert.EqualValues(t, 2, groups[0].MemberCount, "ann + carol (ghost is not a known user)")

	// The admin user queries see the directory source.
	sources, err := repos.Users.DirectorySources(ctx, []uuid.UUID{ann.ID, carol.ID})
	require.NoError(t, err)
	assert.Len(t, sources, 2)

	// Status is recorded on the connection.
	after, err := svc.Get(ctx, conn.ID)
	require.NoError(t, err)
	assert.Equal(t, "ok", after.LastSyncStatus)
	assert.Equal(t, 3, after.LastSyncUsers)

	// A second sync is idempotent: nobody new, same group.
	res2, err := svc.Sync(ctx, uuid.Nil, conn.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, res2.Created)
	assert.Equal(t, 3, res2.Linked)
	_, total, err = svc.ListGroups(ctx, conn.ID, 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)

	// A group dropped from the directory is removed on the next sync.
	fc.groups = nil
	_, err = svc.Sync(ctx, uuid.Nil, conn.ID)
	require.NoError(t, err)
	_, total, err = svc.ListGroups(ctx, conn.ID, 10, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)

	// Deleting the connection keeps the users it created.
	require.NoError(t, svc.Delete(ctx, uuid.Nil, conn.ID))
	_, err = repos.Users.GetByEmail(ctx, annMail)
	require.NoError(t, err)
}

func TestSyncFailureIsRecorded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, &fakeConn{searchErr: assert.AnError})
	conn, err := svc.Create(ctx, uuid.Nil, ldapInput(uniqueName()))
	require.NoError(t, err)

	_, err = svc.Sync(ctx, uuid.Nil, conn.ID)
	require.ErrorIs(t, err, ErrSyncFailed)
	after, err := svc.Get(ctx, conn.ID)
	require.NoError(t, err)
	assert.Equal(t, "error", after.LastSyncStatus)
	assert.NotEmpty(t, after.LastSyncMessage)
}

func TestSAMLConnectionHasNoSync(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newService(t, &fakeConn{})
	cfg, _ := json.Marshal(SAMLConfig{IDPMetadataURL: "https://idp.example.com/metadata"})
	conn, err := svc.Create(ctx, uuid.Nil, Input{Kind: KindSAML, Name: uniqueName(), Enabled: true, Config: cfg})
	require.NoError(t, err)
	assert.False(t, conn.HasSecret)
	_, err = svc.Sync(ctx, uuid.Nil, conn.ID)
	require.ErrorIs(t, err, ErrSyncUnsupported)
}

func TestRecordSAMLLoginTracksGroups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := integrationRepos()
	svc := newService(t, &fakeConn{})

	cfg, _ := json.Marshal(SAMLConfig{IDPMetadataURL: "https://idp.example.com/metadata", GroupsAttribute: "groups"})
	conn, err := svc.Create(ctx, uuid.Nil, Input{Kind: KindSAML, Name: uniqueName(), Enabled: true, Config: cfg})
	require.NoError(t, err)
	user, err := repos.Users.Create(ctx, localUser("saml-"+uuid.NewString()[:8]+"@example.test"))
	require.NoError(t, err)

	counts := func() map[string]int64 {
		rows, _, err := svc.ListGroups(ctx, conn.ID, 50, 0)
		require.NoError(t, err)
		out := map[string]int64{}
		for _, g := range rows {
			out[g.Name] = int64(g.MemberCount)
		}
		return out
	}

	svc.RecordSAMLLogin(ctx, conn.Name, user.ID, "nameid-1", map[string]any{"groups": []string{"eng", "ops"}})
	assert.Equal(t, map[string]int64{"eng": 1, "ops": 1}, counts())

	// The user is linked to the connection (shown as their source).
	sources, err := repos.Users.DirectorySources(ctx, []uuid.UUID{user.ID})
	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.Equal(t, conn.Name, sources[0].ConnectionName)

	// A later sign-in with fewer groups drops the membership it no longer has.
	svc.RecordSAMLLogin(ctx, conn.Name, user.ID, "nameid-1", map[string]any{"groups": []any{"eng"}})
	assert.Equal(t, map[string]int64{"eng": 1, "ops": 0}, counts())

	// A provider that is not a directory connection is ignored.
	svc.RecordSAMLLogin(ctx, "defined-in-config-file", user.ID, "x", map[string]any{"groups": []string{"zzz"}})
	assert.Equal(t, map[string]int64{"eng": 1, "ops": 0}, counts())
}

func TestAuthenticateExternal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := integrationRepos()

	suffix := uuid.NewString()[:8]
	annMail := "ann-" + suffix + "@example.test"
	carolMail := "carol-" + suffix + "@example.test"
	ann := person("ann"+suffix, annMail, "Ann A")
	carol := person("carol"+suffix, carolMail, "Carol C")

	// carol already has a local account with her own password.
	local, err := repos.Users.Create(ctx, localUser(carolMail))
	require.NoError(t, err)

	fc := &fakeConn{
		users:     []ldapEntry{ann, carol},
		passwords: map[string]string{ann.DN: "ann-pw", carol.DN: "carol-pw"},
	}
	// The fake ignores the search filter, so serve only the entry being signed in.
	svc := newService(t, &filteringConn{inner: fc})
	_, err = svc.Create(ctx, uuid.Nil, ldapInput(uniqueName()))
	require.NoError(t, err)

	// First sign-in creates the account: verified, no local password.
	uid, ok := svc.AuthenticateExternal(ctx, annMail, "ann-pw")
	require.True(t, ok)
	created, err := repos.Users.GetByID(ctx, uid)
	require.NoError(t, err)
	assert.Equal(t, annMail, created.Email)
	assert.NotNil(t, created.EmailVerifiedAt)
	assert.Nil(t, created.PasswordHash)

	// Signing in again returns the same account.
	again, ok := svc.AuthenticateExternal(ctx, annMail, "ann-pw")
	require.True(t, ok)
	assert.Equal(t, uid, again)

	// Wrong password, empty password and an unknown user are all refused.
	_, ok = svc.AuthenticateExternal(ctx, annMail, "wrong")
	assert.False(t, ok)
	_, ok = svc.AuthenticateExternal(ctx, annMail, "")
	assert.False(t, ok)
	_, ok = svc.AuthenticateExternal(ctx, "nobody-"+suffix+"@example.test", "x")
	assert.False(t, ok)

	// An existing local account with the same email is linked, not duplicated,
	// and its local password is left alone.
	linked, ok := svc.AuthenticateExternal(ctx, carolMail, "carol-pw")
	require.True(t, ok)
	assert.Equal(t, local.ID, linked)
	after, err := repos.Users.GetByID(ctx, local.ID)
	require.NoError(t, err)
	require.NotNil(t, after.PasswordHash)
	assert.Equal(t, *local.PasswordHash, *after.PasswordHash)
}

func TestUpsertUserWithoutCreationOnlyLinks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := integrationRepos()
	svc := newService(t, &fakeConn{})
	conn, err := svc.Create(ctx, uuid.Nil, ldapInput(uniqueName()))
	require.NoError(t, err)

	suffix := uuid.NewString()[:8]
	// Unknown email and creation off -> not provisioned, nothing created.
	_, _, err = svc.upsertUser(ctx, conn.ID, "uid=new,dc=x", "new-"+suffix+"@example.test", "New", false)
	require.ErrorIs(t, err, errNotProvisioned)
	_, err = repos.Users.GetByEmail(ctx, "new-"+suffix+"@example.test")
	require.Error(t, err)

	// An existing user is linked even with creation off.
	existing, err := repos.Users.Create(ctx, localUser("old-"+suffix+"@example.test"))
	require.NoError(t, err)
	uid, created, err := svc.upsertUser(ctx, conn.ID, "uid=old,dc=x", existing.Email, "Old", false)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, existing.ID, uid)
}

// filteringConn answers a user search with only the entry whose email appears in
// the filter (the real server does this filtering; the fake ignores filters).
type filteringConn struct{ inner *fakeConn }

func (f *filteringConn) Close() { f.inner.Close() }

func (f *filteringConn) Bind(dn, pw string) error { return f.inner.Bind(dn, pw) }

func (f *filteringConn) Search(base, filter string, attrs []string, limit int) ([]ldapEntry, error) {
	all, err := f.inner.Search(base, filter, attrs, limit)
	if err != nil {
		return nil, err
	}
	var out []ldapEntry
	for _, e := range all {
		if strings.Contains(filter, "(mail="+e.first("mail")+")") {
			out = append(out, e)
		}
	}
	return out, nil
}

type recordingProvisioner struct {
	mu    sync.Mutex
	users []uuid.UUID
}

func (r *recordingProvisioner) ProvisionSignup(_ context.Context, id uuid.UUID, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users = append(r.users, id)
	return nil
}

func TestNewDirectoryAccountsAreProvisioned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	fc := &fakeConn{users: []ldapEntry{person("p"+suffix, "p-"+suffix+"@example.test", "P")}}
	svc := newService(t, fc)
	prov := &recordingProvisioner{}
	svc.SetProvisioner(prov)
	conn, err := svc.Create(ctx, uuid.Nil, ldapInput(uniqueName()))
	require.NoError(t, err)

	uid, created, err := svc.upsertUser(ctx, conn.ID, "uid=p"+suffix+",dc=x", "p-"+suffix+"@example.test", "P", true)
	require.NoError(t, err)
	require.True(t, created)
	assert.Equal(t, []uuid.UUID{uid}, prov.users, "a new account is provisioned exactly once")

	// Linking an existing account provisions nothing.
	_, created, err = svc.upsertUser(ctx, conn.ID, "uid=p"+suffix+",dc=x", "p-"+suffix+"@example.test", "P", true)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Len(t, prov.users, 1)
}
