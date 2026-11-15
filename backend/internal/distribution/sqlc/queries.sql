-- name: InsertRun :one
INSERT INTO distribution_runs (split_id, algorithm, seed, roster, result, metrics)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetRun :one
SELECT * FROM distribution_runs WHERE id = $1 AND split_id = $2;

-- name: GetPublishedRun :one
SELECT * FROM distribution_runs WHERE split_id = $1 AND published_at IS NOT NULL;

-- name: ListRunsBySplit :many
SELECT * FROM distribution_runs WHERE split_id = $1 ORDER BY id DESC;

-- name: MarkRunPublished :one
-- published_at IS NULL: повторная публикация не перезаписывает время первой.
UPDATE distribution_runs SET published_at = now(), team_ids = $2, updated_at = now()
WHERE id = $1 AND published_at IS NULL
RETURNING *;
