-- 0021_plugin_permissions: the grant table the permission enforcer consults
-- at every host call (WS-10a). One row per (plugin, permission). Permissions
-- follow the same "scope.action" slug format as RBAC (rbac.Perm*), plus an
-- optional ":qualifier" suffix for resource-scoped permissions
-- ("kv.read:cache", "events.listen:dns.record.*", "api.handler.register:/foo").
--
-- The enforcer does prefix matching on the qualifier so a grant of
-- "events.listen:dns.record.*" satisfies a request for
-- "events.listen:dns.record.created". The wildcard must be the LAST
-- qualifier segment; "*:*" grants the broadest possible scope and is
-- surfaced in the admin UI with a clear warning.
--
-- This table is intentionally separate from role_permissions (RBAC). RBAC
-- answers "what can USER U do in TENANT T?"; this table answers "what can
-- PLUGIN P do?". The two never interact: a plugin does not have a role, and
-- a user's role does not grant plugin permissions. A user with the
-- plugins.permission.approve permission may grant ANY plugin permission to
-- ANY plugin (per WS-10a DoD); the grant itself is recorded here.

CREATE TABLE plugin_permissions (
    -- composite PK: a plugin has at most one row per permission string. The
    -- permission string includes any qualifier ("kv.read:cache"), so
    -- "kv.read:cache" and "kv.read:state" are two distinct grants.
    plugin_id           UUID        NOT NULL REFERENCES plugins (id) ON DELETE CASCADE,
    permission          TEXT        NOT NULL,
    -- who approved this grant (the admin). Required; the audit row for the
    -- same action records the WHY, this row records the WHO for fast lookup.
    granted_by_user_id  UUID        NOT NULL REFERENCES users (id) ON DELETE NO ACTION,
    granted_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plugin_id, permission)
);

-- Hot lookups.
CREATE INDEX idx_plugin_permissions_plugin_id  ON plugin_permissions (plugin_id);
CREATE INDEX idx_plugin_permissions_permission ON plugin_permissions (permission);
CREATE INDEX idx_plugin_permissions_granted_by ON plugin_permissions (granted_by_user_id);

COMMENT ON TABLE  plugin_permissions                       IS 'Admin-approved grants a plugin holds; consulted by the WS-10a enforcer on every host call.';
COMMENT ON COLUMN plugin_permissions.permission            IS 'scope.action[:qualifier] slug; qualifier may end with * for prefix match.';
COMMENT ON COLUMN plugin_permissions.granted_by_user_id    IS 'The admin who approved this grant at install time.';
