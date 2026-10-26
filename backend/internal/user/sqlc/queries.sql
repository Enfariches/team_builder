-- name: CreateUser :one
INSERT INTO users (login, password_hash, first_name, middle_name, last_name, link)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetUserByLogin :one
SELECT * FROM users WHERE login = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUser :one
-- set_* различают «поле не передано» и «передан null»: только во втором случае
-- значение перезаписывается NULL-ом.
UPDATE users SET
    first_name  = COALESCE(sqlc.narg(first_name), first_name),
    last_name   = COALESCE(sqlc.narg(last_name), last_name),
    middle_name = CASE WHEN sqlc.arg(set_middle_name)::bool THEN sqlc.narg(middle_name) ELSE middle_name END,
    link        = CASE WHEN sqlc.arg(set_link)::bool THEN sqlc.narg(link) ELSE link END,
    updated_at  = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListUsersByIDs :many
SELECT * FROM users WHERE id = ANY(sqlc.arg(ids)::bigint[]) ORDER BY id;
