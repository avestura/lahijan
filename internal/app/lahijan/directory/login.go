package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// errNotProvisioned means the credentials are valid in the directory but the
// connection is configured not to create accounts on sign-in and none exists.
var errNotProvisioned = errors.New("directory: no matching Lahijan account")

// maxPasswordLen bounds what is sent to a directory server.
const maxPasswordLen = 1024

var _ session.ExternalAuthenticator = (*Service)(nil)

// AuthenticateExternal verifies an email + password against the enabled LDAP
// connections, in creation order, and returns the Lahijan user the credentials
// belong to. It implements session.ExternalAuthenticator.
//
// A directory user who has no Lahijan account yet gets one on first sign-in
// (unless the connection turns that off); an existing account with the same
// email is linked rather than duplicated. Wrong passwords, unknown users,
// ambiguous matches and directory outages all return ok=false, so a caller
// cannot tell them apart.
func (s *Service) AuthenticateExternal(ctx context.Context, email, password string) (uuid.UUID, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" || len(password) > maxPasswordLen {
		return uuid.Nil, false
	}
	rows, err := s.dir.ListConnections(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "directory sign-in: list connections failed", "err", err)
		return uuid.Nil, false
	}
	// The list is newest first; try the oldest connection first.
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if row.Kind != KindLDAP || !row.Enabled {
			continue
		}
		if uid, ok := s.authenticateLDAP(ctx, row, email, password); ok {
			return uid, true
		}
	}
	return uuid.Nil, false
}

// authenticateLDAP checks the credentials against one connection and resolves
// the Lahijan user.
func (s *Service) authenticateLDAP(ctx context.Context, row gen.DirectoryConnection, email, password string) (uuid.UUID, bool) {
	var cfg LDAPConfig
	if err := json.Unmarshal(row.Config, &cfg); err != nil {
		s.log.WarnContext(ctx, "directory sign-in: bad connection config", "connection", row.Name, "err", err)
		return uuid.Nil, false
	}
	cfg = cfg.withDefaults()
	servicePassword, err := s.openSecret(row)
	if err != nil {
		s.log.WarnContext(ctx, "directory sign-in: cannot read bind password", "connection", row.Name, "err", err)
		return uuid.Nil, false
	}

	entry, ok := s.verifyLDAPCredentials(ctx, row.Name, cfg, servicePassword, email, password)
	if !ok {
		return uuid.Nil, false
	}
	name := strings.TrimSpace(entry.first(cfg.NameAttr))
	if name == "" {
		name = strings.TrimSpace(entry.first("cn"))
	}
	uid, _, err := s.upsertUser(ctx, row.ID, normDN(entry.DN), email, name, cfg.createOnLogin())
	if err != nil {
		if !errors.Is(err, errNotProvisioned) {
			s.log.WarnContext(ctx, "directory sign-in: resolve user failed", "connection", row.Name, "err", err)
		}
		return uuid.Nil, false
	}
	return uid, true
}

// verifyLDAPCredentials finds the one directory entry whose email attribute is
// email and proves the password by binding as it. The search runs as the
// connection's service account; the bind uses a separate connection so the
// service connection's identity is never reused for the user.
func (s *Service) verifyLDAPCredentials(
	ctx context.Context, connName string, cfg LDAPConfig, servicePassword, email, password string,
) (ldapEntry, bool) {
	searchConn, err := s.dial(cfg, servicePassword)
	if err != nil {
		s.log.WarnContext(ctx, "directory sign-in: cannot reach the directory", "connection", connName, "err", err)
		return ldapEntry{}, false
	}
	defer searchConn.Close()

	entries, err := searchConn.Search(cfg.UserBaseDN, userLookupFilter(cfg, email),
		[]string{"dn", cfg.EmailAttr, cfg.NameAttr, "cn"}, 2)
	if err != nil {
		s.log.WarnContext(ctx, "directory sign-in: user search failed", "connection", connName, "err", err)
		return ldapEntry{}, false
	}
	// Zero matches: unknown user. More than one: ambiguous, so refuse rather
	// than guess whose password this is.
	if len(entries) != 1 {
		return ldapEntry{}, false
	}
	entry := entries[0]

	bindConn, err := s.dial(cfg, servicePassword)
	if err != nil {
		s.log.WarnContext(ctx, "directory sign-in: cannot reach the directory", "connection", connName, "err", err)
		return ldapEntry{}, false
	}
	defer bindConn.Close()
	if err := bindConn.Bind(entry.DN, password); err != nil {
		return ldapEntry{}, false
	}
	return entry, true
}

// userLookupFilter combines the connection's user filter with an exact match
// on the (escaped) email attribute.
func userLookupFilter(cfg LDAPConfig, email string) string {
	base := strings.TrimSpace(cfg.UserFilter)
	if !strings.HasPrefix(base, "(") {
		base = "(" + base + ")"
	}
	return fmt.Sprintf("(&%s(%s=%s))", base, cfg.EmailAttr, escapeFilter(email))
}
