-- name: CreateUser :one
INSERT INTO users (id, name, email, password_hash, role, active)
VALUES ($1, $2, LOWER(TRIM(sqlc.arg('email')::text)), $3, $4, $5)
RETURNING id, name, email, password_hash, role, active, created_at, updated_at;

-- name: GetUserByID :one
SELECT id, name, email, password_hash, role, active, created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetUserByEmail :one
SELECT id, name, email, password_hash, role, active, created_at, updated_at
FROM users
WHERE LOWER(email) = LOWER(TRIM(sqlc.arg('email')::text));

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CountUsersByFilter :one
SELECT COUNT(*)
FROM users
WHERE (sqlc.narg('role')::text IS NULL OR role = sqlc.narg('role'));

-- name: ListUsers :many
SELECT id, name, email, password_hash, role, active, created_at, updated_at
FROM users
WHERE (sqlc.narg('role')::text IS NULL OR role = sqlc.narg('role'))
ORDER BY created_at DESC, id DESC
LIMIT $1 OFFSET $2;

-- name: UpdateUser :one
UPDATE users
SET
    name = $2,
    role = $3,
    active = $4,
    password_hash = $5,
    updated_at = NOW()
WHERE id = $1
RETURNING id, name, email, password_hash, role, active, created_at, updated_at;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, user_id, family_id, token_hash, expires_at, revoked_at, replaced_by_id, created_at;

-- name: GetRefreshTokenByHashForUpdate :one
SELECT id, user_id, family_id, token_hash, expires_at, revoked_at, replaced_by_id, created_at
FROM refresh_tokens
WHERE token_hash = $1
FOR UPDATE;

-- name: GetRefreshTokenByHash :one
SELECT id, user_id, family_id, token_hash, expires_at, revoked_at, replaced_by_id, created_at
FROM refresh_tokens
WHERE token_hash = $1;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET
    revoked_at = NOW(),
    replaced_by_id = $2
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeRefreshTokenFamily :exec
UPDATE refresh_tokens
SET revoked_at = NOW()
WHERE family_id = $1 AND revoked_at IS NULL;

-- name: RevokeAllUserRefreshTokens :exec
UPDATE refresh_tokens
SET revoked_at = NOW()
WHERE user_id = $1 AND revoked_at IS NULL;
