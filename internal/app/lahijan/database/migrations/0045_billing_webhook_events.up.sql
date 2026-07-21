-- 0045_billing_webhook_events: idempotent Stripe webhook ingestion log (WS-27).
--
-- Every Stripe webhook delivery that passes signature verification gets
-- one row here. The UNIQUE on stripe_event_id is the idempotency
-- boundary: a duplicate Stripe delivery (Stripe retries up to ~3 days)
-- hits the constraint and the handler returns 200 OK without re-applying
-- the effect.
--
-- The table is append-only at the application layer (we never UPDATE or
-- DELETE; a re-process is a NEW row with a re-process marker). The
-- status column carries the disposition:
--
--   received       the row was inserted; processing not started
--   applied        the ledger effect (if any) was applied successfully
--   duplicate      the event was already applied; ignored
--   failed         processing raised an error; the handler returned 5xx
--                  so Stripe retries the delivery
--
-- This table is the source of truth for "did we process this event". The
-- ledger_entries table is the source of truth for "what happened to the
-- balance".

CREATE TABLE billing_webhook_events (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID        REFERENCES tenants (id) ON DELETE CASCADE,

    -- The Stripe event id (e.g. "evt_1AbCdE..."). Unique platform-wide
    -- because Stripe never reuses event ids.
    stripe_event_id     TEXT        NOT NULL,

    -- The Stripe event type (e.g. "payment_intent.succeeded",
    -- "invoice.paid", "charge.dispute.created").
    stripe_event_type   TEXT        NOT NULL,

    -- The Stripe API version the event was generated under (e.g.
    -- "2024-06-20"). Cached so a future re-process knows which schema
    -- the payload conforms to.
    stripe_api_version  TEXT,

    -- The full event payload (JSON). Used for replay + audit. Stored as
    -- JSONB so a re-process can pick out fields with SQL if needed.
    payload             JSONB       NOT NULL,

    -- Disposition (see table comment).
    status              TEXT        NOT NULL DEFAULT 'received',

    -- The ledger entry id(s) this event produced, when applicable.
    -- Array so a single event can produce multiple effects (rare but
    -- possible — e.g. a dispute creates a debit + a fee).
    ledger_entry_ids    UUID[]      NOT NULL DEFAULT '{}',

    -- Free-form error context (the failed status carries the error
    -- message here).
    error_message       TEXT,

    -- When the webhook was received (= when the row was inserted).
    received_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- When processing completed (success or failure).
    processed_at        TIMESTAMPTZ
);

-- The idempotency boundary.
CREATE UNIQUE INDEX uq_billing_webhook_events_stripe_id ON billing_webhook_events (stripe_event_id);
-- Hot lookups: by type for the dashboard / admin ops.
CREATE INDEX        idx_billing_webhook_events_type      ON billing_webhook_events (stripe_event_type, received_at DESC);
CREATE INDEX        idx_billing_webhook_events_tenant    ON billing_webhook_events (tenant_id, received_at DESC);

COMMENT ON TABLE  billing_webhook_events                  IS 'Idempotent Stripe webhook ingestion log (WS-27, ADR-0034).';
COMMENT ON COLUMN billing_webhook_events.stripe_event_id  IS 'Stripe event id; UNIQUE = idempotency boundary.';
COMMENT ON COLUMN billing_webhook_events.status           IS 'received | applied | duplicate | failed.';
COMMENT ON COLUMN billing_webhook_events.ledger_entry_ids IS 'Ledger entries produced by this event (rarely >1).';
