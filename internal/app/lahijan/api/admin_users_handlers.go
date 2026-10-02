// Package api: admin_users_handlers.go implements the platform-admin user
// management endpoints:
//
//	GET    /api/v1/admin/users                                  -> ListAdminUsers
//	POST   /api/v1/admin/users                                  -> CreateAdminUser
//	GET    /api/v1/admin/users/{userId}                         -> GetAdminUser
//	PATCH  /api/v1/admin/users/{userId}                         -> UpdateAdminUser
//	DELETE /api/v1/admin/users/{userId}                         -> DeleteAdminUser
//	PUT    /api/v1/admin/users/{userId}/memberships/{tenantId}  -> SetAdminUserRole
//	GET    /api/v1/permissions                                  -> ListPermissions
//
// The admin paths are gated by AuditGate (platform.user.list /
// platform.user.manage). Handlers stay thin: parse, call the repositories /
// session service, render the DTO.
package api

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
)

// ListPermissions handles GET /api/v1/permissions. Any authenticated user may
// read the catalogue; it contains no secrets and drives checklists in the UI.
func (s *Server) ListPermissions(c *fiber.Ctx) error {
	if _, ok := currentUserID(c); !ok {
		return SendUnauthorized(c, i18n.T(c.UserContext(), "auth.err_unauthorized", nil))
	}
	perms := rbac.AllPermissions()
	items := make([]apigen.PermissionInfo, 0, len(perms))
	for _, p := range perms {
		items = append(items, apigen.PermissionInfo{Slug: p.Slug, Description: p.Description})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Slug < items[j].Slug })
	return c.JSON(apigen.PermissionList{Items: items})
}

// ListAdminUsers handles GET /api/v1/admin/users.
func (s *Server) ListAdminUsers(c *fiber.Ctx, params apigen.ListAdminUsersParams) error {
	ctx := c.UserContext()
	limit, offset := pageParams(params.Limit, params.Offset)
	q := ""
	if params.Q != nil {
		q = strings.TrimSpace(*params.Q)
	}
	rows, err := s.users.Search(ctx, q, limit, offset)
	if err != nil {
		logUnexpectedError(c, "admin.users.list", err)
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	total, err := s.users.CountSearch(ctx, q)
	if err != nil {
		logUnexpectedError(c, "admin.users.count", err)
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	items, err := s.adminUserDTOs(ctx, rows)
	if err != nil {
		logUnexpectedError(c, "admin.users.enrich", err)
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	return c.JSON(apigen.AdminUserPage{Items: items, Total: total, Limit: int(limit), Offset: int(offset)})
}

// GetAdminUser handles GET /api/v1/admin/users/{userId}.
func (s *Server) GetAdminUser(c *fiber.Ctx, userID openapi_types.UUID) error {
	dto, err := s.loadAdminUser(c.UserContext(), userID)
	if err != nil {
		return s.mapAdminUserError(c, err)
	}
	return c.JSON(dto)
}

// CreateAdminUser handles POST /api/v1/admin/users.
func (s *Server) CreateAdminUser(c *fiber.Ctx) error {
	ctx := c.UserContext()
	actor, ok := currentUserID(c)
	if !ok {
		return SendUnauthorized(c, i18n.T(ctx, "auth.err_unauthorized", nil))
	}
	var req apigen.AdminUserCreateRequest
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.Email) == "" {
		return SendBadRequest(c, i18n.T(ctx, "admin.err_bad_request", nil), nil)
	}
	in := session.AdminCreateUserInput{
		Email:       req.Email,
		Password:    strPtrOr(req.Password, ""),
		DisplayName: req.DisplayName,
		IsActive:    req.IsActive,
	}
	if req.Locale != nil {
		in.Locale = string(*req.Locale)
	}
	user, err := s.sessionSvc.AdminCreateUser(ctx, in)
	if err != nil {
		return s.mapAuthError(c, err)
	}
	s.emitAdminUserAudit(ctx, audit.ActionAdminUserCreate, actor, user.ID, map[string]any{"email": user.Email})
	dto, err := s.loadAdminUser(ctx, user.ID)
	if err != nil {
		return s.mapAdminUserError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(dto)
}

// UpdateAdminUser handles PATCH /api/v1/admin/users/{userId}.
func (s *Server) UpdateAdminUser(c *fiber.Ctx, userID openapi_types.UUID) error {
	ctx := c.UserContext()
	actor, ok := currentUserID(c)
	if !ok {
		return SendUnauthorized(c, i18n.T(ctx, "auth.err_unauthorized", nil))
	}
	var req apigen.AdminUserUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(ctx, "admin.err_bad_request", nil), nil)
	}
	if _, err := s.activeUser(ctx, userID); err != nil {
		return s.mapAdminUserError(c, err)
	}
	if req.IsActive != nil && !*req.IsActive && actor == userID {
		return SendError(c, fiber.StatusConflict, CodeConflict, i18n.T(ctx, "admin.err_self_action", nil), nil)
	}
	meta := map[string]any{}
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		var ptr *string
		if name != "" {
			ptr = &name
		}
		if err := s.users.UpdateDisplayName(ctx, userID, ptr); err != nil {
			return s.mapAdminUserError(c, err)
		}
		meta["display_name"] = name
	}
	if req.IsActive != nil {
		if err := s.users.SetActive(ctx, userID, *req.IsActive); err != nil {
			return s.mapAdminUserError(c, err)
		}
		if !*req.IsActive {
			// A disabled user must not keep working sessions.
			_ = s.sessions.RevokeAllForUser(ctx, userID)
		}
		meta["is_active"] = *req.IsActive
	}
	s.emitAdminUserAudit(ctx, audit.ActionAdminUserUpdate, actor, userID, meta)
	dto, err := s.loadAdminUser(ctx, userID)
	if err != nil {
		return s.mapAdminUserError(c, err)
	}
	return c.JSON(dto)
}

// DeleteAdminUser handles DELETE /api/v1/admin/users/{userId}.
func (s *Server) DeleteAdminUser(c *fiber.Ctx, userID openapi_types.UUID) error {
	ctx := c.UserContext()
	actor, ok := currentUserID(c)
	if !ok {
		return SendUnauthorized(c, i18n.T(ctx, "auth.err_unauthorized", nil))
	}
	if actor == userID {
		return SendError(c, fiber.StatusConflict, CodeConflict, i18n.T(ctx, "admin.err_self_action", nil), nil)
	}
	u, err := s.activeUser(ctx, userID)
	if err != nil {
		return s.mapAdminUserError(c, err)
	}
	if err := s.users.SoftDelete(ctx, userID); err != nil {
		return s.mapAdminUserError(c, err)
	}
	_ = s.sessions.RevokeAllForUser(ctx, userID)
	s.emitAdminUserAudit(ctx, audit.ActionAdminUserDelete, actor, userID, map[string]any{"email": u.Email})
	return c.SendStatus(fiber.StatusNoContent)
}

// SetAdminUserRole handles PUT /api/v1/admin/users/{userId}/memberships/{tenantId}.
func (s *Server) SetAdminUserRole(c *fiber.Ctx, userID, tenantID openapi_types.UUID) error {
	ctx := c.UserContext()
	actor, ok := currentUserID(c)
	if !ok {
		return SendUnauthorized(c, i18n.T(ctx, "auth.err_unauthorized", nil))
	}
	var req apigen.AdminUserRoleRequest
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.Role) == "" {
		return SendBadRequest(c, i18n.T(ctx, "admin.err_bad_request", nil), nil)
	}
	if _, err := s.activeUser(ctx, userID); err != nil {
		return s.mapAdminUserError(c, err)
	}
	role, err := s.rbac.GetRoleBySlug(ctx, strings.TrimSpace(req.Role))
	if err != nil {
		if database.IsNoRows(err) {
			return SendBadRequest(c, i18n.T(ctx, "admin.err_role_unknown", nil), nil)
		}
		logUnexpectedError(c, "admin.users.role", err)
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	tctx := database.WithTenant(ctx, tenantID)
	if _, mErr := s.memberships.GetByUser(tctx, userID); mErr != nil {
		if database.IsNoRows(mErr) {
			return SendNotFound(c, i18n.T(ctx, "admin.err_not_member", nil))
		}
		logUnexpectedError(c, "admin.users.role", mErr)
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	roleID := role.ID
	if sErr := s.memberships.SetRole(tctx, userID, &roleID); sErr != nil {
		logUnexpectedError(c, "admin.users.role", sErr)
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	s.emitAdminUserAudit(ctx, audit.ActionAdminUserRoleUpdate, actor, userID,
		map[string]any{"tenant_id": tenantID.String(), "role": role.Slug})
	dto, err := s.loadAdminUser(ctx, userID)
	if err != nil {
		return s.mapAdminUserError(c, err)
	}
	return c.JSON(dto)
}

// --- helpers -------------------------------------------------------------

// errAdminUserNotFound marks a missing (or soft-deleted) user.
var errAdminUserNotFound = errors.New("admin: user not found")

// activeUser returns the user, treating soft-deleted rows as missing.
func (s *Server) activeUser(ctx context.Context, id uuid.UUID) (gen.User, error) {
	u, err := s.users.GetByID(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return gen.User{}, errAdminUserNotFound
		}
		return gen.User{}, err
	}
	if u.DeletedAt != nil {
		return gen.User{}, errAdminUserNotFound
	}
	return u, nil
}

func (s *Server) loadAdminUser(ctx context.Context, id uuid.UUID) (apigen.AdminUser, error) {
	u, err := s.activeUser(ctx, id)
	if err != nil {
		return apigen.AdminUser{}, err
	}
	dtos, err := s.adminUserDTOs(ctx, []gen.User{u})
	if err != nil {
		return apigen.AdminUser{}, err
	}
	return dtos[0], nil
}

// adminUserDTOs renders a page of users with their memberships and directory
// source in two extra queries (not one per user).
func (s *Server) adminUserDTOs(ctx context.Context, users []gen.User) ([]apigen.AdminUser, error) {
	ids := make([]uuid.UUID, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	members, err := s.users.MembershipDetails(ctx, ids)
	if err != nil {
		return nil, err
	}
	sources, err := s.users.DirectorySources(ctx, ids)
	if err != nil {
		return nil, err
	}
	byUser := make(map[uuid.UUID][]apigen.AdminUserMembership, len(users))
	for _, m := range members {
		byUser[m.UserID] = append(byUser[m.UserID], apigen.AdminUserMembership{
			TenantId: m.TenantID, TenantSlug: m.TenantSlug, TenantName: m.TenantName, Role: m.RoleSlug,
		})
	}
	srcByUser := make(map[uuid.UUID]apigen.AdminUserDirectorySource, len(sources))
	for _, src := range sources {
		if _, seen := srcByUser[src.UserID]; seen {
			continue
		}
		srcByUser[src.UserID] = apigen.AdminUserDirectorySource{
			ConnectionId: src.ConnectionID, Name: src.ConnectionName, Kind: apigen.AdminUserDirectorySourceKind(src.Kind),
		}
	}
	out := make([]apigen.AdminUser, 0, len(users))
	for _, u := range users {
		hasPw := u.PasswordHash != nil && *u.PasswordHash != ""
		dto := apigen.AdminUser{
			Id: u.ID, Email: u.Email, DisplayName: u.DisplayName, Locale: u.Locale,
			IsActive: u.IsActive, EmailVerified: u.EmailVerifiedAt != nil, HasPassword: &hasPw,
			CreatedAt: u.CreatedAt, Memberships: byUser[u.ID],
		}
		if dto.Memberships == nil {
			dto.Memberships = []apigen.AdminUserMembership{}
		}
		if src, ok := srcByUser[u.ID]; ok {
			src := src
			dto.DirectorySource = &src
		}
		out = append(out, dto)
	}
	return out, nil
}

func (s *Server) mapAdminUserError(c *fiber.Ctx, err error) error {
	if errors.Is(err, errAdminUserNotFound) {
		return SendNotFound(c, i18n.T(c.UserContext(), "admin.err_user_not_found", nil))
	}
	logUnexpectedError(c, "admin.users", err)
	return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
}

func (s *Server) emitAdminUserAudit(ctx context.Context, action string, actor, target uuid.UUID, meta map[string]any) {
	if s.auditEmitter == nil {
		return
	}
	ev := audit.Event{
		Action: action, ResourceType: audit.ResourceUser, ResourceID: &target,
		Status: audit.StatusSuccess, ActorType: audit.ActorUser, Metadata: meta,
	}
	if actor != uuid.Nil {
		ev.ActorUserID = &actor
	}
	_, _ = s.auditEmitter.Emit(ctx, ev)
}
