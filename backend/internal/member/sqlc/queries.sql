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

-- name: AssignMembers :execrows
-- Одно обращение на всех участников публикации: id, команда и роль идут
-- параллельными массивами и склеиваются по порядковому номеру.
UPDATE members SET
    team_id       = u.team_id,
    assigned_role = u.role,
    updated_at    = now()
FROM (
    SELECT i.id, t.team_id, r.role
    FROM unnest(sqlc.arg(ids)::bigint[]) WITH ORDINALITY AS i(id, n)
    JOIN unnest(sqlc.arg(team_ids)::bigint[]) WITH ORDINALITY AS t(team_id, n) USING (n)
    JOIN unnest(sqlc.arg(roles)::text[]) WITH ORDINALITY AS r(role, n) USING (n)
) AS u
WHERE members.id = u.id AND members.split_id = sqlc.arg(split_id);

-- name: ListMemberAssignments :many
-- Без form_snapshot: модуль team получает только состав, не анкеты.
SELECT id, user_id, team_id, assigned_role FROM members WHERE split_id = $1 ORDER BY id;
