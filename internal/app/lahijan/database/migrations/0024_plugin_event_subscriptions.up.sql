-- 0024_plugin_event_subscriptions: durable event subscriptions for plugins
-- (WS-10b). A plugin subscribes to a topic pattern via the events host
-- function; the row persists the subscription so events emitted while the
-- plugin is disabled (or the process is down) are delivered when it
-- resumes. The audit log listens separately (always in-process) and does
-- not need a row here.
--
-- Per WS-10b "subscription delivery goes through the existing River queues
-- (durable)", the bus enqueues a job per matched subscription; this table
-- is the registry the bus consults at emit time to find the match list.
-- A subscription row that is "paused" (the plugin is disabled) is still
-- matched, but the bus drops the payload into a bounded backlog (capped
-- per-plugin in conf.wasm.events.*) rather than the live queue. The
-- backlog itself is held in River's queue with a custom job kind so it
-- inherits River's retry + DLQ semantics; this table is the durable
-- "what does the plugin care about" registry.
--
-- tenant_id is NULLABLE for platform-wide plugins.

CREATE TABLE plugin_event_subscriptions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        REFERENCES tenants (id) ON DELETE CASCADE,
    plugin_id       UUID        NOT NULL REFERENCES plugins (id) ON DELETE CASCADE,
    -- The topic pattern. May be exact ("dns.record.created") or a
    -- prefix wildcard ending in ".*" ("dns.record.*"). Matching is
    -- done by eventbus.TopicPattern.Match — same algorithm as the
    -- permission enforcer.
    topic_pattern   TEXT        NOT NULL,
    -- The WASM function the host should call when a matching event
    -- fires. The function must be in the plugin's manifest entrypoints.
    handler         TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A plugin can subscribe to the same pattern with different handlers
-- (rare but valid); the unique constraint is on the full tuple.
CREATE UNIQUE INDEX uq_plugin_event_subs_plugin_topic_handler
    ON plugin_event_subscriptions (plugin_id, topic_pattern, handler);

CREATE INDEX idx_plugin_event_subs_plugin_id ON plugin_event_subscriptions (plugin_id);
CREATE INDEX idx_plugin_event_subs_tenant_id ON plugin_event_subscriptions (tenant_id) WHERE tenant_id IS NOT NULL;

COMMENT ON TABLE  plugin_event_subscriptions                IS 'Durable event subscriptions; backs the events host function (WS-10b).';
COMMENT ON COLUMN plugin_event_subscriptions.topic_pattern IS 'Exact topic or prefix wildcard ending in ".*". Matched by eventbus.TopicPattern.Match.';
COMMENT ON COLUMN plugin_event_subscriptions.handler        IS 'WASM export called when a matching event fires.';
