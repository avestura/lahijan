-- mfa_pending_sessions: short-lived, single-use pending session tokens
-- issued during login when the user has MFA enabled (WS-07c).

-- name: CreateMFAPendingSession :one
INSERT INTO mfa_pending_sessions (
    user_id,
    token_hash,
    expires_at,
    user_agent,
    ip_address
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetMFAPendingSessionByHash :one
SELECT * FROM mfa_pending_sessions WHERE token_hash = $1;

-- name: ConsumeMFAPendingSession :exec
UPDATE mfa_pending_sessions
SET consumed_at = now()
WHERE id = $1 AND consumed_at IS NULL AND revoked_at IS NULL;

-- name: RevokeMFAPendingSession :exec
UPDATE mfa_pending_sessions
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: IncMFAPendingSessionFailures :exec
-- Bumps the failure counter; the caller checks if it crosses the threshold
-- and calls RevokeMFAPendingSession to lock the user out.
UPDATE mfa_pending_sessions
SET failed_attempts = failed_attempts + 1
WHERE id = $1;

-- name: DeleteMFAPendingSessionsForUser :exec
DELETE FROM mfa_pending_sessions WHERE user_id = $1;
