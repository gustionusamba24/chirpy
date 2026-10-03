-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email, hashed_password)
VALUES (
    gen_random_uuid(), 
    NOW(), 
    NOW(), 
    $1,
    $2
)
RETURNING id, created_at, updated_at, email, hashed_password, is_chirpy_red;

-- name: DeleteAllUsers :exec
DELETE FROM users;

-- name: GetUserById :one
SELECT id,
       created_at,
       updated_at,
       email,
       hashed_password,
       is_chirpy_red
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id,
       created_at,
       updated_at,
       email,
       hashed_password,
       is_chirpy_red
FROM users
WHERE email = $1;

-- name: UpdateUser :one
UPDATE users
SET email = $2,
    hashed_password = $3,
    updated_at = NOW()
WHERE id = $1
RETURNING id, created_at, updated_at, email, hashed_password, is_chirpy_red;

-- name: UpgradeUserToChirpyRed :one
UPDATE users
SET is_chirpy_red = TRUE,
    updated_at = NOW()
WHERE id = $1
RETURNING id, created_at, updated_at, email, hashed_password, is_chirpy_red;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (token, created_at, updated_at, user_id, expires_at, revoked_at)
VALUES ($1, 
       NOW(), 
       NOW(), 
       $2, 
       $3, 
       NULL
)
RETURNING token, created_at, updated_at, user_id, expires_at, revoked_at;

-- name: GetUserFromRefreshToken :one
SELECT users.id,
       users.created_at,
       users.updated_at,
       users.email,
       users.hashed_password,
       users.is_chirpy_red
FROM users
JOIN refresh_tokens ON refresh_tokens.user_id = users.id
WHERE refresh_tokens.token = $1
    AND refresh_tokens.expires_at > NOW()
    AND refresh_tokens.revoked_at IS NULL;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = NOW(),
    updated_at = NOW()
WHERE token = $1;