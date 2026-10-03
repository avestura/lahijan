package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/saml"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// pgUniqueViolation is the SQLSTATE for unique_violation.
const pgUniqueViolation = "23505"

// Connection is the domain view of a directory connection. Secrets are never
// part of it: HasSecret only says whether one is stored.
type Connection struct {
	ID        uuid.UUID
	Kind      string
	Name      string
	Enabled   bool
	Config    json.RawMessage
	HasSecret bool
	// Active reports whether the connection is live: an enabled LDAP
	// connection, or an enabled SAML connection registered for sign-in.
	Active bool
	// ActivationError is why a SAML connection could not be activated by the
	// create / update that just ran. It is not stored.
	ActivationError string

	LastSyncAt      *time.Time
	LastSyncStatus  string
	LastSyncMessage string
	LastSyncUsers   int
	LastSyncGroups  int

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Input is the editable definition of a connection. BindPassword is only
// consulted for LDAP; nil on update keeps the stored one.
type Input struct {
	Kind         string
	Name         string
	Enabled      bool
	Config       json.RawMessage
	BindPassword *string
}

// TestResult is the outcome of a connection test.
type TestResult struct {
	OK bool
	// Code is a stable machine code the UI translates: ok, connect_failed,
	// search_failed, metadata_failed.
	Code string
	// Detail is the underlying error text (admin-only endpoint).
	Detail string
	// Users is how many user entries the filter matched, capped at testSample
	// (LDAP only).
	Users int
	// Groups is how many group entries matched, capped at testSample.
	Groups int
	// EntityID / SSOURL are reported from SAML IdP metadata.
	EntityID string
	SSOURL   string
}

// SyncResult is the outcome of an LDAP sync.
type SyncResult struct {
	Users   int // entries processed
	Created int // new Lahijan users
	Linked  int // existing Lahijan users matched by email and linked
	Skipped int // entries without an email
	Groups  int
}

const testSample = 50

// Service manages directory connections.
type Service struct {
	dir    *database.DirectoryRepository
	users  *database.UsersRepository
	crypto *secrets.Crypto
	audit  audit.Emitter
	log    *slog.Logger
	dial   ldapDialer
	saml   SAMLActivator
	// provisioner, when set, runs for every account this service creates (an
	// LDAP sync or first sign-in), e.g. to give it a personal tenant.
	provisioner Provisioner
}

// Provisioner sets up what a brand-new account needs beyond its user row.
// It is the same hook self-registration uses.
type Provisioner interface {
	ProvisionSignup(ctx context.Context, userID uuid.UUID, email string) error
}

// SetProvisioner installs the post-creation hook. Call once at bootstrap.
func (s *Service) SetProvisioner(p Provisioner) { s.provisioner = p }

// New builds the service. crypto may be nil; creating an LDAP connection with a
// bind password then fails with ErrCryptoRequired.
func New(repos *database.Repos, crypto *secrets.Crypto, emitter audit.Emitter, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		dir: repos.Directory, users: repos.Users, crypto: crypto, audit: emitter, log: log, dial: dialLDAP,
	}
}

// List returns every connection.
func (s *Service) List(ctx context.Context) ([]Connection, error) {
	rows, err := s.dir.ListConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("directory.list: %w", err)
	}
	out := make([]Connection, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.decorate(toConnection(r)))
	}
	return out, nil
}

// Get returns one connection.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Connection, error) {
	row, err := s.dir.GetConnection(ctx, id)
	if err != nil {
		return Connection{}, mapNotFound(err)
	}
	return s.decorate(toConnection(row)), nil
}

// Create validates and stores a connection.
func (s *Service) Create(ctx context.Context, actor uuid.UUID, in Input) (Connection, error) {
	if err := ValidateName(in.Name); err != nil {
		return Connection{}, err
	}
	cfg, err := normalizeConfig(in.Kind, in.Config)
	if err != nil {
		return Connection{}, err
	}
	secret, err := s.sealSecret(in.Kind, in.BindPassword)
	if err != nil {
		return Connection{}, err
	}
	params := database.CreateConnectionParams{
		Kind: in.Kind, Name: in.Name, Enabled: in.Enabled, Config: cfg, Secret: secret,
	}
	if actor != uuid.Nil {
		params.CreatedBy = &actor
	}
	row, err := s.dir.CreateConnection(ctx, params)
	if err != nil {
		return Connection{}, mapWriteErr(err)
	}
	s.emit(ctx, audit.ActionDirectoryConnectionCreate, row.ID, actor, map[string]any{"kind": row.Kind, "name": row.Name})
	return s.withActivation(row, s.applySAMLChange(nil, &row)), nil
}

// Update replaces the editable fields of a connection. The kind cannot change.
func (s *Service) Update(ctx context.Context, actor, id uuid.UUID, in Input) (Connection, error) {
	cur, err := s.dir.GetConnection(ctx, id)
	if err != nil {
		return Connection{}, mapNotFound(err)
	}
	if nameErr := ValidateName(in.Name); nameErr != nil {
		return Connection{}, nameErr
	}
	cfg, err := normalizeConfig(cur.Kind, in.Config)
	if err != nil {
		return Connection{}, err
	}
	secret, err := s.sealSecret(cur.Kind, in.BindPassword)
	if err != nil {
		return Connection{}, err
	}
	row, err := s.dir.UpdateConnection(ctx, database.UpdateConnectionParams{
		ID: id, Name: in.Name, Enabled: in.Enabled, Config: cfg, Secret: secret,
	})
	if err != nil {
		return Connection{}, mapWriteErr(err)
	}
	s.emit(ctx, audit.ActionDirectoryConnectionUpdate, id, actor, map[string]any{"kind": row.Kind, "name": row.Name})
	return s.withActivation(row, s.applySAMLChange(&cur, &row)), nil
}

// Delete removes a connection together with its imported groups and links.
// Users that were imported are kept.
func (s *Service) Delete(ctx context.Context, actor, id uuid.UUID) error {
	cur, err := s.dir.GetConnection(ctx, id)
	if err != nil {
		return mapNotFound(err)
	}
	if err := s.dir.DeleteConnection(ctx, id); err != nil {
		return fmt.Errorf("directory.delete: %w", err)
	}
	_ = s.applySAMLChange(&cur, nil)
	s.emit(ctx, audit.ActionDirectoryConnectionDelete, id, actor, map[string]any{"kind": cur.Kind, "name": cur.Name})
	return nil
}

// Test checks a connection without changing anything. Pass either a saved
// connection id, or a draft Input (with optional id to reuse the stored bind
// password when the draft leaves it empty).
func (s *Service) Test(ctx context.Context, actor uuid.UUID, id *uuid.UUID, draft *Input) (TestResult, error) {
	var kind string
	var rawCfg json.RawMessage
	var password string

	switch {
	case draft != nil:
		kind, rawCfg = draft.Kind, draft.Config
		if draft.BindPassword != nil {
			password = *draft.BindPassword
		}
		if id != nil && draft.BindPassword == nil {
			row, err := s.dir.GetConnection(ctx, *id)
			if err != nil {
				return TestResult{}, mapNotFound(err)
			}
			if kind == "" {
				kind = row.Kind
			}
			pw, err := s.openSecret(row)
			if err != nil {
				return TestResult{}, err
			}
			password = pw
		}
	case id != nil:
		row, err := s.dir.GetConnection(ctx, *id)
		if err != nil {
			return TestResult{}, mapNotFound(err)
		}
		kind, rawCfg = row.Kind, row.Config
		pw, err := s.openSecret(row)
		if err != nil {
			return TestResult{}, err
		}
		password = pw
	default:
		return TestResult{}, fmt.Errorf("%w: nothing to test", ErrInvalid)
	}

	cfg, err := normalizeConfig(kind, rawCfg)
	if err != nil {
		return TestResult{}, err
	}
	var res TestResult
	switch kind {
	case KindLDAP:
		res = s.testLDAP(cfg, password)
	case KindSAML:
		res = testSAML(cfg)
	}
	meta := map[string]any{"kind": kind, "ok": res.OK, "code": res.Code}
	rid := uuid.Nil
	if id != nil {
		rid = *id
	}
	s.emit(ctx, audit.ActionDirectoryConnectionTest, rid, actor, meta)
	return res, nil
}

func (s *Service) testLDAP(raw json.RawMessage, password string) TestResult {
	var cfg LDAPConfig
	_ = json.Unmarshal(raw, &cfg) // already validated by normalizeConfig
	conn, err := s.dial(cfg, password)
	if err != nil {
		return TestResult{Code: "connect_failed", Detail: err.Error()}
	}
	defer conn.Close()

	users, err := conn.Search(cfg.UserBaseDN, cfg.UserFilter, []string{"dn", cfg.EmailAttr}, testSample)
	if err != nil {
		return TestResult{Code: "search_failed", Detail: err.Error()}
	}
	res := TestResult{OK: true, Code: "ok", Users: len(users)}
	if cfg.GroupBaseDN != "" {
		groups, err := conn.Search(cfg.GroupBaseDN, cfg.GroupFilter, []string{"dn", cfg.GroupNameAttr}, testSample)
		if err != nil {
			return TestResult{Code: "search_failed", Detail: err.Error(), Users: len(users)}
		}
		res.Groups = len(groups)
	}
	return res
}

func testSAML(raw json.RawMessage) TestResult {
	var cfg SAMLConfig
	_ = json.Unmarshal(raw, &cfg) // already validated by normalizeConfig
	info, err := saml.CheckIDPMetadata(cfg.IDPMetadataXML, cfg.IDPMetadataURL)
	if err != nil {
		return TestResult{Code: "metadata_failed", Detail: err.Error()}
	}
	return TestResult{OK: true, Code: "ok", EntityID: info.EntityID, SSOURL: info.SSOURL}
}

// Sync imports users and groups from an LDAP connection. Users that vanished
// from the directory are left untouched (disable them from user management).
func (s *Service) Sync(ctx context.Context, actor, id uuid.UUID) (SyncResult, error) {
	row, err := s.dir.GetConnection(ctx, id)
	if err != nil {
		return SyncResult{}, mapNotFound(err)
	}
	if row.Kind != KindLDAP {
		return SyncResult{}, ErrSyncUnsupported
	}
	var cfg LDAPConfig
	if decodeErr := json.Unmarshal(row.Config, &cfg); decodeErr != nil {
		return SyncResult{}, fmt.Errorf("directory.sync: decode config: %w", decodeErr)
	}
	cfg = cfg.withDefaults()
	password, err := s.openSecret(row)
	if err != nil {
		return SyncResult{}, err
	}

	res, syncErr := s.runLDAPSync(ctx, row.ID, cfg, password)
	status, msg := "ok", ""
	if syncErr != nil {
		status, msg = "error", syncErr.Error()
	}
	if err := s.dir.SetSyncResult(ctx, id, status, msg, int32(res.Users), int32(res.Groups)); err != nil { //nolint:gosec // bounded by directory size
		s.log.WarnContext(ctx, "directory sync: record result failed", "connection", id, "err", err)
	}
	meta := map[string]any{
		"status": status, "users": res.Users, "created": res.Created, "linked": res.Linked, "groups": res.Groups,
	}
	s.emit(ctx, audit.ActionDirectoryConnectionSync, id, actor, meta)
	if syncErr != nil {
		return res, fmt.Errorf("%w: %w", ErrSyncFailed, syncErr)
	}
	return res, nil
}

func (s *Service) runLDAPSync(ctx context.Context, connID uuid.UUID, cfg LDAPConfig, password string) (SyncResult, error) {
	var res SyncResult
	conn, err := s.dial(cfg, password)
	if err != nil {
		return res, err
	}
	defer conn.Close()

	entries, err := conn.Search(cfg.UserBaseDN, cfg.UserFilter, []string{"dn", cfg.EmailAttr, cfg.NameAttr, "cn"}, 0)
	if err != nil {
		return res, err
	}

	dnToUser := make(map[string]uuid.UUID, len(entries))
	for _, e := range entries {
		email := strings.ToLower(strings.TrimSpace(e.first(cfg.EmailAttr)))
		if email == "" {
			res.Skipped++
			continue
		}
		name := strings.TrimSpace(e.first(cfg.NameAttr))
		if name == "" {
			name = strings.TrimSpace(e.first("cn"))
		}
		uid, created, upErr := s.upsertUser(ctx, connID, normDN(e.DN), email, name, true)
		if upErr != nil {
			return res, fmt.Errorf("user %s: %w", email, upErr)
		}
		dnToUser[normDN(e.DN)] = uid
		res.Users++
		if created {
			res.Created++
		}
	}
	res.Linked = res.Users - res.Created

	if cfg.GroupBaseDN == "" {
		return res, nil
	}
	groups, err := conn.Search(cfg.GroupBaseDN, cfg.GroupFilter,
		[]string{"dn", cfg.GroupNameAttr, cfg.GroupMemberAttr, "description"}, 0)
	if err != nil {
		return res, err
	}
	keep := make([]string, 0, len(groups))
	for _, g := range groups {
		name := strings.TrimSpace(g.first(cfg.GroupNameAttr))
		if name == "" {
			name = g.DN
		}
		row, err := s.dir.UpsertGroup(ctx, connID, normDN(g.DN), name, g.first("description"))
		if err != nil {
			return res, fmt.Errorf("group %s: %w", name, err)
		}
		keep = append(keep, normDN(g.DN))
		var members []uuid.UUID
		for _, dn := range g.all(cfg.GroupMemberAttr) {
			if uid, ok := dnToUser[normDN(dn)]; ok {
				members = append(members, uid)
			}
		}
		if err := s.dir.ReplaceGroupMembers(ctx, row.ID, members); err != nil {
			return res, fmt.Errorf("group %s members: %w", name, err)
		}
		res.Groups++
	}
	if err := s.dir.DeleteStaleGroups(ctx, connID, keep); err != nil {
		return res, fmt.Errorf("drop stale groups: %w", err)
	}
	return res, nil
}

// upsertUser finds or creates the Lahijan user for a directory entry and links
// it. created reports whether a new user row was made.
func (s *Service) upsertUser(ctx context.Context, connID uuid.UUID, externalID, email, name string, createMissing bool) (uuid.UUID, bool, error) {
	var display *string
	if name != "" {
		display = &name
	}
	if link, err := s.dir.FindLinkedUser(ctx, connID, externalID); err == nil {
		return link.UserID, false, nil
	} else if !database.IsNoRows(err) {
		return uuid.Nil, false, err
	}
	existing, err := s.users.GetByEmail(ctx, email)
	switch {
	case err == nil:
		if linkErr := s.dir.LinkUser(ctx, connID, existing.ID, externalID); linkErr != nil {
			return uuid.Nil, false, linkErr
		}
		return existing.ID, false, nil
	case !database.IsNoRows(err):
		return uuid.Nil, false, err
	}
	if !createMissing {
		return uuid.Nil, false, errNotProvisioned
	}
	u, err := s.users.Create(ctx, database.CreateUserParams{Email: email, DisplayName: display})
	if err != nil {
		return uuid.Nil, false, err
	}
	// The directory vouches for the address, so no verification email is needed.
	if err := s.users.VerifyEmail(ctx, u.ID); err != nil {
		return uuid.Nil, false, err
	}
	if s.provisioner != nil {
		// Best-effort, like self-registration: the account exists either way.
		if perr := s.provisioner.ProvisionSignup(ctx, u.ID, email); perr != nil {
			s.log.WarnContext(ctx, "directory: provisioning a new account failed", "user", u.ID, "err", perr)
		}
	}
	if err := s.dir.LinkUser(ctx, connID, u.ID, externalID); err != nil {
		return uuid.Nil, false, err
	}
	return u.ID, true, nil
}

// ListGroups returns a page of a connection's groups with member counts.
func (s *Service) ListGroups(ctx context.Context, id uuid.UUID, limit, offset int32) ([]gen.ListDirectoryGroupsRow, int64, error) {
	if _, err := s.dir.GetConnection(ctx, id); err != nil {
		return nil, 0, mapNotFound(err)
	}
	rows, err := s.dir.ListGroups(ctx, id, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("directory.groups: %w", err)
	}
	total, err := s.dir.CountGroups(ctx, id)
	if err != nil {
		return nil, 0, fmt.Errorf("directory.groups: count: %w", err)
	}
	return rows, total, nil
}

// --- helpers -------------------------------------------------------------

func (s *Service) sealSecret(kind string, pw *string) ([]byte, error) {
	if kind != KindLDAP || pw == nil || *pw == "" {
		return nil, nil
	}
	if s.crypto == nil {
		return nil, ErrCryptoRequired
	}
	sealed, err := s.crypto.Seal(*pw)
	if err != nil {
		return nil, fmt.Errorf("directory: seal secret: %w", err)
	}
	return []byte(sealed), nil
}

func (s *Service) openSecret(row gen.DirectoryConnection) (string, error) {
	if len(row.SecretEncrypted) == 0 {
		return "", nil
	}
	if s.crypto == nil {
		return "", ErrCryptoRequired
	}
	pw, err := s.crypto.Open(string(row.SecretEncrypted))
	if err != nil {
		return "", fmt.Errorf("directory: open secret: %w", err)
	}
	return pw, nil
}

func (s *Service) emit(ctx context.Context, action string, resourceID, actor uuid.UUID, meta map[string]any) {
	ev := audit.Event{
		Action: action, ResourceType: audit.ResourceDirectoryConnection,
		Status: audit.StatusSuccess, ActorType: audit.ActorUser, Metadata: meta,
	}
	if actor != uuid.Nil {
		ev.ActorUserID = &actor
	}
	if resourceID != uuid.Nil {
		rid := resourceID
		ev.ResourceID = &rid
	}
	// Audit must never roll back the action; the emitter already logged.
	_, _ = s.audit.Emit(ctx, ev)
}

// withActivation builds the view of a just-written row, carrying the
// activation error (if any) from reconciling the SAML registry.
func (s *Service) withActivation(row gen.DirectoryConnection, activationErr error) Connection {
	c := s.decorate(toConnection(row))
	if activationErr != nil {
		c.ActivationError = activationErr.Error()
	}
	return c
}

func toConnection(r gen.DirectoryConnection) Connection {
	c := Connection{
		ID: r.ID, Kind: r.Kind, Name: r.Name, Enabled: r.Enabled, Config: r.Config,
		HasSecret: len(r.SecretEncrypted) > 0, LastSyncAt: r.LastSyncAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.LastSyncStatus != nil {
		c.LastSyncStatus = *r.LastSyncStatus
	}
	if r.LastSyncMessage != nil {
		c.LastSyncMessage = *r.LastSyncMessage
	}
	if r.LastSyncUsers != nil {
		c.LastSyncUsers = int(*r.LastSyncUsers)
	}
	if r.LastSyncGroups != nil {
		c.LastSyncGroups = int(*r.LastSyncGroups)
	}
	return c
}

func mapNotFound(err error) error {
	if database.IsNoRows(err) {
		return ErrNotFound
	}
	return err
}

func mapWriteErr(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == pgUniqueViolation {
		return ErrNameTaken
	}
	return err
}

// normDN canonicalises a DN for matching (directory DNs are case-insensitive
// and may differ in spacing after commas).
func normDN(dn string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(dn)), ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ",")
}
