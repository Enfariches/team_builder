-- name: InsertInvite :one
INSERT INTO invites (split_id, token, expires_at, max_uses)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListInvitesBySplit :many
SELECT * FROM invites WHERE split_id = $1 ORDER BY created_at DESC, id DESC;

-- name: GetInviteByToken :one
SELECT * FROM invites WHERE token = $1;

-- name: GetInvite :one
SELECT * FROM invites WHERE id = $1 AND split_id = $2;

-- name: RevokeInvite :execrows
-- COALESCE: повторный отзыв не перезаписывает время первого.
UPDATE invites SET
    revoked_at = COALESCE(revoked_at, sqlc.arg(now)::timestamptz),
    updated_at = now()
WHERE id = sqlc.arg(id) AND split_id = sqlc.arg(split_id);

-- name: RedeemInvite :one
-- Проверка действительности и увеличение счётчика в одном UPDATE: параллельные
-- попытки на последнее использование не пройдут обе.
UPDATE invites SET
    uses_count = uses_count + 1,
    updated_at = now()
WHERE token = sqlc.arg(token)
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > sqlc.arg(now)::timestamptz)
  AND (max_uses IS NULL OR uses_count < max_uses)
RETURNING *;

-- name: ReleaseInvite :execrows
UPDATE invites SET
    uses_count = GREATEST(uses_count - 1, 0),
    updated_at = now()
WHERE id = $1;
