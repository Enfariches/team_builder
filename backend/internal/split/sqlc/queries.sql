-- name: InsertSplit :one
INSERT INTO splits (owner_id, title, description, team_size, max_members, remainder_policy, auto_finish_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: InsertRole :one
INSERT INTO split_roles (split_id, name, per_team_min, per_team_max, sort_order)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSplit :one
SELECT * FROM splits WHERE id = $1;

-- name: GetSplitForUpdate :one
SELECT * FROM splits WHERE id = $1 FOR UPDATE;

-- name: UpdateSplit :one
-- set_* различают «поле не передано» и «передан null»: только во втором случае
-- значение перезаписывается NULL-ом.
UPDATE splits SET
    title            = COALESCE(sqlc.narg(title), title),
    team_size        = COALESCE(sqlc.narg(team_size), team_size),
    remainder_policy = COALESCE(sqlc.narg(remainder_policy), remainder_policy),
    description      = CASE WHEN sqlc.arg(set_description)::bool THEN sqlc.narg(description) ELSE description END,
    max_members      = CASE WHEN sqlc.arg(set_max_members)::bool THEN sqlc.narg(max_members) ELSE max_members END,
    auto_finish_at   = CASE WHEN sqlc.arg(set_auto_finish_at)::bool THEN sqlc.narg(auto_finish_at) ELSE auto_finish_at END,
    updated_at       = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateSplitStatus :one
UPDATE splits SET status = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: DeleteSplit :exec
DELETE FROM splits WHERE id = $1;

-- name: DeleteRolesBySplit :exec
DELETE FROM split_roles WHERE split_id = $1;

-- name: ListRolesBySplitIDs :many
SELECT * FROM split_roles WHERE split_id = ANY(sqlc.arg(split_ids)::bigint[]) ORDER BY split_id, sort_order, id;

-- name: ListSplitsForUser :many
SELECT * FROM splits
WHERE (owner_id = sqlc.arg(user_id) OR id = ANY(sqlc.arg(member_ids)::bigint[]))
  AND (sqlc.narg(role)::text IS NULL
       OR (sqlc.narg(role)::text = 'owner' AND owner_id = sqlc.arg(user_id))
       OR (sqlc.narg(role)::text = 'member' AND owner_id <> sqlc.arg(user_id)))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
ORDER BY created_at DESC, id DESC;
