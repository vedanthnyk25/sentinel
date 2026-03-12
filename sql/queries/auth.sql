-- name: CreateUser :exec
-- Creates a new user with the given email and password hash.
INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3);


-- name: GetUserByEmail :one
-- Retrieves a user by their email address.
SELECT id, email, password_hash
FROM users
WHERE email = $1;
