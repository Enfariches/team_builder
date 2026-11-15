-- name: InsertTeam :one
INSERT INTO teams (split_id, name, sort_order)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListTeamsBySplit :many
SELECT * FROM teams WHERE split_id = $1 ORDER BY sort_order, id;

-- name: CountTeamsBySplits :many
SELECT split_id, count(*) AS teams_count
FROM teams
WHERE split_id = ANY(sqlc.arg(split_ids)::bigint[])
GROUP BY split_id;
