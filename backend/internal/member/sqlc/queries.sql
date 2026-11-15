-- name: LockSplit :exec
-- Advisory-lock на пространство до конца транзакции: сериализует вступления
-- и выходы, не трогая таблицу splits (она принадлежит модулю split).
SELECT pg_advisory_xact_lock(sqlc.arg(split_id)::bigint);

-- name: InsertMember :one
INSERT INTO members (split_id, user_id, form_id, form_snapshot)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetMember :one
SELECT * FROM members WHERE id = $1;

-- name: GetMemberByUser :one
SELECT * FROM members WHERE split_id = $1 AND user_id = $2;

-- name: ListMembersBySplit :many
SELECT * FROM members WHERE split_id = $1 ORDER BY created_at, id;

-- name: CountMembersBySplit :one
SELECT count(*) FROM members WHERE split_id = $1;

-- name: CountMembersBySplits :many
SELECT split_id, count(*) AS members_count
FROM members
WHERE split_id = ANY(sqlc.arg(split_ids)::bigint[])
GROUP BY split_id;

-- name: SplitIDsOfUser :many
SELECT split_id FROM members WHERE user_id = $1;

-- name: IsMember :one
SELECT EXISTS (SELECT 1 FROM members WHERE split_id = $1 AND user_id = $2);

-- name: DeleteMember :execrows
DELETE FROM members WHERE id = $1;
