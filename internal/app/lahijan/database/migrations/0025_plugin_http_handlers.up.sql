-- 0025_plugin_http_handlers: HTTP handler mounts registered by plugins
-- (WS-10b). Backs the api.register_handler host function. One row per
-- (plugin, method, path). The HTTP layer (api package) consults this
-- table on every request to /api/v1/plugins/<plugin-slug>/... to find
-- the handler; on a hit it instantiates the plugin, calls the named
-- export, and returns the result through the standard pipeline.
--
-- When the plugin is disabled (status = "disabled" in plugins) the
-- HTTP layer skips its rows entirely so the route effectively 404s.
-- When the plugin is deleted, CASCADE removes its rows so no orphan
-- mounts remain. This is the WS-10b DoD item "register_handler mounts
-- a new HTTP route dynamically (and removes it when the plugin is
-- disabled)".
--
-- tenant_id is NULLABLE for platform-wide plugins.

CREATE TABLE plugin_http_handlers (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        REFERENCES tenants (id) ON DELETE CASCADE,
    plugin_id       UUID        NOT NULL REFERENCES plugins (id) ON DELETE CASCADE,
    -- HTTP method (GET, POST, ...). Stored uppercase. The host function
    -- upper-cases before insert so case-insensitive matching in the
    -- router does not surprise anyone.
    method          TEXT        NOT NULL,
    -- The sub-path under /api/v1/plugins/<plugin-slug>/. Must start
    -- with "/"; the host function rejects paths that try to escape the
    -- prefix (no "//", no "..").
    path            TEXT        NOT NULL,
    -- The WASM function exported by the plugin that handles the
    -- request. The HTTP layer calls this export with the request body
    -- and reads the response body from the plugin's memory.
    handler         TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A plugin cannot register the same (method, path) twice.
CREATE UNIQUE INDEX uq_plugin_http_handlers_plugin_method_path
    ON plugin_http_handlers (plugin_id, method, path);

CREATE INDEX idx_plugin_http_handlers_plugin_id ON plugin_http_handlers (plugin_id);
CREATE INDEX idx_plugin_http_handlers_tenant_id ON plugin_http_handlers (tenant_id) WHERE tenant_id IS NOT NULL;
-- Hot lookup for the router: dispatch by (method, path) across every
-- plugin. The router further filters by tenant visibility at runtime.
CREATE INDEX idx_plugin_http_handlers_method_path ON plugin_http_handlers (method, path);

COMMENT ON TABLE  plugin_http_handlers          IS 'HTTP handler mounts registered by plugins; backs api.register_handler (WS-10b).';
COMMENT ON COLUMN plugin_http_handlers.method   IS 'HTTP method (uppercase).';
COMMENT ON COLUMN plugin_http_handlers.path     IS 'Sub-path under /api/v1/plugins/<plugin-slug>/; must start with /.';
COMMENT ON COLUMN plugin_http_handlers.handler  IS 'WASM export called for each matching request.';
