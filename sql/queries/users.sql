-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email, hashed_password)
VALUES (
    gen_random_uuid(), 
    NOW(), 
    NOW(), 
    $1,
    $2
)
RETURNING *;

-- name: DeleteAllUsers :exec
DELETE FROM users;

-- name: GetUserById :one
SELECT id,
       created_at,
       updated_at,
       email,
       hashed_password
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id,
       created_at,
       updated_at,
       email,
       hashed_password
FROM users
WHERE email = $1;