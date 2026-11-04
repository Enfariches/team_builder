-- name: InsertForm :one
INSERT INTO forms (user_id, name, experience, skills, role_preferences, comment)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetForm :one
SELECT * FROM forms WHERE id = $1 AND user_id = $2;

-- name: ListFormsByUser :many
SELECT * FROM forms WHERE user_id = $1 ORDER BY created_at, id;

-- name: UpdateForm :one
-- set_comment различает «поле не передано» и «передан null»: только во втором
-- случае комментарий перезаписывается NULL-ом.
UPDATE forms SET
    name             = COALESCE(sqlc.narg(name), name),
    experience       = COALESCE(sqlc.narg(experience)::smallint, experience),
    skills           = COALESCE(sqlc.narg(skills)::text[], skills),
    role_preferences = COALESCE(sqlc.narg(role_preferences)::jsonb, role_preferences),
    comment          = CASE WHEN sqlc.arg(set_comment)::bool THEN sqlc.narg(comment) ELSE comment END,
    updated_at       = now()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteForm :execrows
DELETE FROM forms WHERE id = $1 AND user_id = $2;
