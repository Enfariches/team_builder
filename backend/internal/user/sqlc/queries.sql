-- name: CreateUser :one
INSERT INTO users (login, password_hash, first_name, middle_name, last_name, link)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetUserByLogin :one
SELECT * FROM users WHERE login = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;
