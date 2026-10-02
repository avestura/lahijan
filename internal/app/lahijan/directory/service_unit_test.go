package directory

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
)

// newDraftService builds a Service that never touches the database: it can
// run a draft Test (no saved connection) and validate input.
func newDraftService(conn ldapConn, dialErr error) *Service {
	return &Service{
		audit: audit.NoopEmitter{},
		log:   nil,
		dial: func(LDAPConfig, string) (ldapConn, error) {
			if dialErr != nil {
				return nil, dialErr
			}
			return conn, nil
		},
	}
}

func draftLDAP(t *testing.T) *Input {
	t.Helper()
	return &Input{Kind: KindLDAP, Name: "corp", Enabled: true, Config: ldapCfg(t, nil)}
}

func TestTestDraftLDAP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fc := &fakeConn{users: []ldapEntry{{DN: "uid=a"}}, groups: []ldapEntry{{DN: "cn=g"}}}
	pw := "secret"
	in := draftLDAP(t)
	in.BindPassword = &pw
	res, err := newDraftService(fc, nil).Test(ctx, uuid.Nil, nil, in)
	require.NoError(t, err)
	assert.True(t, res.OK)
	assert.Equal(t, 1, res.Users)
	assert.Equal(t, 1, res.Groups)

	res, err = newDraftService(nil, ErrLDAPConnect).Test(ctx, uuid.Nil, nil, draftLDAP(t))
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.Equal(t, "connect_failed", res.Code)
}

func TestTestDraftSAMLWithBadMetadata(t *testing.T) {
	t.Parallel()
	in := &Input{Kind: KindSAML, Name: "sso", Config: json.RawMessage(`{"idpMetadataXml":"<not-metadata/>"}`)}
	res, err := newDraftService(nil, nil).Test(context.Background(), uuid.Nil, nil, in)
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.Equal(t, "metadata_failed", res.Code)
	assert.NotEmpty(t, res.Detail)
}

func TestTestRejectsNothingAndInvalidDrafts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newDraftService(nil, nil)

	_, err := svc.Test(ctx, uuid.Nil, nil, nil)
	require.ErrorIs(t, err, ErrInvalid)

	_, err = svc.Test(ctx, uuid.Nil, nil, &Input{Kind: KindLDAP, Config: json.RawMessage(`{"url":"http://x"}`)})
	require.ErrorIs(t, err, ErrInvalid)

	_, err = svc.Test(ctx, uuid.Nil, nil, &Input{Kind: "radius", Config: json.RawMessage(`{}`)})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestCreateValidatesBeforeStoring(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newDraftService(nil, nil)

	_, err := svc.Create(ctx, uuid.Nil, Input{Kind: KindLDAP, Name: "Bad Name", Config: ldapCfg(t, nil)})
	require.ErrorIs(t, err, ErrInvalid)

	_, err = svc.Create(ctx, uuid.Nil, Input{Kind: KindLDAP, Name: "ok", Config: json.RawMessage(`{`)})
	require.ErrorIs(t, err, ErrInvalid)

	// A bind password cannot be stored without an encryption key.
	pw := "secret"
	_, err = svc.Create(ctx, uuid.Nil, Input{Kind: KindLDAP, Name: "ok", Config: ldapCfg(t, nil), BindPassword: &pw})
	require.ErrorIs(t, err, ErrCryptoRequired)
}

func TestSealSecret(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	empty := ""
	pw := "x"

	got, err := svc.sealSecret(KindSAML, &pw)
	require.NoError(t, err)
	assert.Nil(t, got, "SAML has no secret")
	got, err = svc.sealSecret(KindLDAP, nil)
	require.NoError(t, err)
	assert.Nil(t, got, "a nil password keeps the stored one")
	got, err = svc.sealSecret(KindLDAP, &empty)
	require.NoError(t, err)
	assert.Nil(t, got)
	_, err = svc.sealSecret(KindLDAP, &pw)
	require.ErrorIs(t, err, ErrCryptoRequired)
}

func TestActivateSAMLWithoutActivatorIsANoOp(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	svc.ActivateSAML(context.Background()) // must not touch the (nil) repository
	svc.SetSAMLActivator(newFakeActivator())
	assert.NotNil(t, svc.saml)
}

func TestMapWriteErrAndNotFound(t *testing.T) {
	t.Parallel()
	assert.NoError(t, mapWriteErr(nil))
	boom := errors.New("boom")
	assert.Equal(t, boom, mapWriteErr(boom))
	assert.Equal(t, boom, mapNotFound(boom))
}
