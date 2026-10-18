-- name: InsertRefreshToken :one
INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at, user_agent)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetRefreshTokenForUpdate :one
SELECT * FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = now(), replaced_by = $2, updated_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeRefreshFamily :exec
UPDATE refresh_tokens
SET revoked_at = now(), updated_at = now()
WHERE family_id = $1 AND revoked_at IS NULL;
