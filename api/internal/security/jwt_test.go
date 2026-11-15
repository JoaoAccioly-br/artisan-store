package security_test

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/security"
)

const testJWTSecret = "super-secret-jwt-key-for-tests-32bytes"

func TestNewJWTManager(t *testing.T) {
	t.Run("rejects secret shorter than 32 chars", func(t *testing.T) {
		for _, secret := range []string{"", "short", strings.Repeat("a", 31)} {
			mgr, err := security.NewJWTManager(secret, 15*time.Minute)
			assert.Nil(t, mgr)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "at least 32 characters")
		}
	})

	t.Run("rejects non-positive TTL", func(t *testing.T) {
		for _, ttl := range []time.Duration{0, -1 * time.Second} {
			mgr, err := security.NewJWTManager(testJWTSecret, ttl)
			assert.Nil(t, mgr)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "positive")
		}
	})

	t.Run("accepts exact 32-char secret and positive TTL", func(t *testing.T) {
		mgr, err := security.NewJWTManager(strings.Repeat("k", 32), time.Minute)
		require.NoError(t, err)
		require.NotNil(t, mgr)
	})
}

func TestJWTManager_GenerateAndParseAccessToken(t *testing.T) {
	mgr, err := security.NewJWTManager(testJWTSecret, 15*time.Minute)
	require.NoError(t, err)

	userID, err := uuid.NewV7()
	require.NoError(t, err)

	user := domain.User{
		ID:    userID,
		Email: "maria@doceria.com",
		Role:  domain.RoleAdmin,
	}

	tokenStr, expiresIn, err := mgr.GenerateAccessToken(user)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenStr)
	assert.Equal(t, 900, expiresIn)

	claims, err := mgr.ParseAccessToken(tokenStr)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, "maria@doceria.com", claims.Email)
	assert.Equal(t, domain.RoleAdmin, claims.Role)
	assert.Equal(t, "artisan-store-api", claims.Issuer)
	assert.Equal(t, userID.String(), claims.Subject)
}

func TestJWTManager_ParseAccessToken_RejectsInvalidTokens(t *testing.T) {
	mgr, err := security.NewJWTManager(testJWTSecret, 15*time.Minute)
	require.NoError(t, err)

	validUID, err := uuid.NewV7()
	require.NoError(t, err)

	signCustom := func(method jwt.SigningMethod, key any, claims security.AccessClaims) string {
		t.Helper()
		tok := jwt.NewWithClaims(method, claims)
		signed, signErr := tok.SignedString(key)
		require.NoError(t, signErr)
		return signed
	}

	now := time.Now().UTC()

	cases := []struct {
		name  string
		token string
	}{
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "malformed token",
			token: "not.a.jwt",
		},
		{
			name: "expired token",
			token: signCustom(jwt.SigningMethodHS256, []byte(testJWTSecret), security.AccessClaims{
				UserID: validUID,
				Email:  "maria@doceria.com",
				Role:   domain.RoleAdmin,
				RegisteredClaims: jwt.RegisteredClaims{
					Issuer:    "artisan-store-api",
					Subject:   validUID.String(),
					IssuedAt:  jwt.NewNumericDate(now.Add(-20 * time.Minute)),
					NotBefore: jwt.NewNumericDate(now.Add(-20 * time.Minute)),
					ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Minute)),
				},
			}),
		},
		{
			name: "not yet valid (nbf in future)",
			token: signCustom(jwt.SigningMethodHS256, []byte(testJWTSecret), security.AccessClaims{
				UserID: validUID,
				Email:  "maria@doceria.com",
				Role:   domain.RoleAdmin,
				RegisteredClaims: jwt.RegisteredClaims{
					Issuer:    "artisan-store-api",
					Subject:   validUID.String(),
					IssuedAt:  jwt.NewNumericDate(now),
					NotBefore: jwt.NewNumericDate(now.Add(10 * time.Minute)),
					ExpiresAt: jwt.NewNumericDate(now.Add(20 * time.Minute)),
				},
			}),
		},
		{
			name: "divergent secret",
			token: signCustom(jwt.SigningMethodHS256, []byte("different-secret-with-at-least-32-chars"), security.AccessClaims{
				UserID: validUID,
				Email:  "maria@doceria.com",
				Role:   domain.RoleAdmin,
				RegisteredClaims: jwt.RegisteredClaims{
					Issuer:    "artisan-store-api",
					Subject:   validUID.String(),
					IssuedAt:  jwt.NewNumericDate(now),
					ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
				},
			}),
		},
		{
			name: "invalid issuer",
			token: signCustom(jwt.SigningMethodHS256, []byte(testJWTSecret), security.AccessClaims{
				UserID: validUID,
				Email:  "maria@doceria.com",
				Role:   domain.RoleAdmin,
				RegisteredClaims: jwt.RegisteredClaims{
					Issuer:    "rogue-issuer",
					Subject:   validUID.String(),
					IssuedAt:  jwt.NewNumericDate(now),
					ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
				},
			}),
		},
		{
			name: "invalid algorithm HS512",
			token: signCustom(jwt.SigningMethodHS512, []byte(testJWTSecret), security.AccessClaims{
				UserID: validUID,
				Email:  "maria@doceria.com",
				Role:   domain.RoleAdmin,
				RegisteredClaims: jwt.RegisteredClaims{
					Issuer:    "artisan-store-api",
					Subject:   validUID.String(),
					IssuedAt:  jwt.NewNumericDate(now),
					ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
				},
			}),
		},
		{
			name: "alg none attack",
			token: signCustom(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, security.AccessClaims{
				UserID: validUID,
				Email:  "maria@doceria.com",
				Role:   domain.RoleAdmin,
				RegisteredClaims: jwt.RegisteredClaims{
					Issuer:    "artisan-store-api",
					Subject:   validUID.String(),
					IssuedAt:  jwt.NewNumericDate(now),
					ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
				},
			}),
		},
		{
			name: "corrupted claim uuid.Nil",
			token: signCustom(jwt.SigningMethodHS256, []byte(testJWTSecret), security.AccessClaims{
				UserID: uuid.Nil,
				Email:  "maria@doceria.com",
				Role:   domain.RoleAdmin,
				RegisteredClaims: jwt.RegisteredClaims{
					Issuer:    "artisan-store-api",
					IssuedAt:  jwt.NewNumericDate(now),
					ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
				},
			}),
		},
		{
			name: "corrupted claim invalid role",
			token: signCustom(jwt.SigningMethodHS256, []byte(testJWTSecret), security.AccessClaims{
				UserID: validUID,
				Email:  "maria@doceria.com",
				Role:   domain.Role("superadmin"),
				RegisteredClaims: jwt.RegisteredClaims{
					Issuer:    "artisan-store-api",
					Subject:   validUID.String(),
					IssuedAt:  jwt.NewNumericDate(now),
					ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
				},
			}),
		},
		{
			name: "corrupted claim empty role",
			token: signCustom(jwt.SigningMethodHS256, []byte(testJWTSecret), security.AccessClaims{
				UserID: validUID,
				Email:  "maria@doceria.com",
				Role:   "",
				RegisteredClaims: jwt.RegisteredClaims{
					Issuer:    "artisan-store-api",
					Subject:   validUID.String(),
					IssuedAt:  jwt.NewNumericDate(now),
					ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
				},
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims, parseErr := mgr.ParseAccessToken(tc.token)
			assert.Nil(t, claims)
			require.Error(t, parseErr)

			domErr, ok := domain.AsDomainError(parseErr)
			require.True(t, ok)
			assert.Equal(t, domain.CodeUnauthorized, domErr.Code)
		})
	}
}

func TestGenerateOpaqueRefreshTokenAndHashToken(t *testing.T) {
	raw1, hash1, err := security.GenerateOpaqueRefreshToken()
	require.NoError(t, err)
	raw2, hash2, err := security.GenerateOpaqueRefreshToken()
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(raw1, "rt_"))
	assert.True(t, strings.HasPrefix(raw2, "rt_"))
	assert.NotEqual(t, raw1, raw2)
	assert.NotEqual(t, hash1, hash2)

	assert.Equal(t, security.HashToken(raw1), hash1)
	assert.Equal(t, security.HashToken(raw2), hash2)
	assert.Len(t, hash1, 64)

	decoded, err := hex.DecodeString(hash1)
	require.NoError(t, err)
	assert.Len(t, decoded, 32)
}
