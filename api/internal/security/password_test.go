package security_test

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/security"
)

func testArgon2idParams() security.Argon2idParams {
	return security.Argon2idParams{
		Memory:      8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}
}

func TestDefaultArgon2idParams(t *testing.T) {
	params := security.DefaultArgon2idParams()
	assert.Equal(t, uint32(64*1024), params.Memory)
	assert.Equal(t, uint32(3), params.Iterations)
	assert.Equal(t, uint8(2), params.Parallelism)
	assert.Equal(t, uint32(16), params.SaltLength)
	assert.Equal(t, uint32(32), params.KeyLength)

	hasher := security.NewArgon2idHasher(params)
	encoded, err := hasher.Hash("SenhaForte#123")
	require.NoError(t, err)

	ok, err := hasher.Verify("SenhaForte#123", encoded)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestArgon2idHasher_Hash(t *testing.T) {
	hasher := security.NewArgon2idHasher(testArgon2idParams())

	t.Run("generates valid PHC format with unique salts", func(t *testing.T) {
		h1, err := hasher.Hash("MinhaSenhaSegura#1")
		require.NoError(t, err)
		h2, err := hasher.Hash("MinhaSenhaSegura#1")
		require.NoError(t, err)

		assert.NotEqual(t, h1, h2, "unique random salts must produce different PHC strings")

		parts := strings.Split(h1, "$")
		require.Len(t, parts, 6)
		assert.Empty(t, parts[0])
		assert.Equal(t, "argon2id", parts[1])
		assert.Equal(t, "v=19", parts[2])
		assert.Equal(t, "m=8192,t=1,p=1", parts[3])

		salt, err := base64.RawStdEncoding.DecodeString(parts[4])
		require.NoError(t, err)
		assert.Len(t, salt, 16)

		digest, err := base64.RawStdEncoding.DecodeString(parts[5])
		require.NoError(t, err)
		assert.Len(t, digest, 32)
	})

	t.Run("rejects empty password", func(t *testing.T) {
		encoded, err := hasher.Hash("")
		assert.Empty(t, encoded)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "password cannot be empty")
	})
}

func TestArgon2idHasher_Verify(t *testing.T) {
	hasher := security.NewArgon2idHasher(testArgon2idParams())
	validHash, err := hasher.Hash("Correta#2026")
	require.NoError(t, err)

	t.Run("returns true for matching password", func(t *testing.T) {
		ok, verifyErr := hasher.Verify("Correta#2026", validHash)
		require.NoError(t, verifyErr)
		assert.True(t, ok)
	})

	t.Run("returns false for wrong password", func(t *testing.T) {
		ok, verifyErr := hasher.Verify("Errada#2026", validHash)
		require.NoError(t, verifyErr)
		assert.False(t, ok)
	})

	t.Run("returns false without error for empty inputs", func(t *testing.T) {
		ok, verifyErr := hasher.Verify("", validHash)
		require.NoError(t, verifyErr)
		assert.False(t, ok)

		ok, verifyErr = hasher.Verify("Correta#2026", "")
		require.NoError(t, verifyErr)
		assert.False(t, ok)
	})

	t.Run("returns error for malformed or unsupported PHC strings", func(t *testing.T) {
		validSalt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
		validDigest := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))

		invalidCases := []struct {
			name    string
			encoded string
			errText string
		}{
			{
				name:    "too few parts",
				encoded: "$argon2id$v=19$m=8192,t=1,p=1$salt",
				errText: "invalid argon2id hash format",
			},
			{
				name:    "too many parts",
				encoded: fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s$extra", validSalt, validDigest),
				errText: "invalid argon2id hash format",
			},
			{
				name:    "wrong algorithm argon2i",
				encoded: fmt.Sprintf("$argon2i$v=19$m=8192,t=1,p=1$%s$%s", validSalt, validDigest),
				errText: "invalid argon2id hash format",
			},
			{
				name:    "invalid version syntax",
				encoded: fmt.Sprintf("$argon2id$v=abc$m=8192,t=1,p=1$%s$%s", validSalt, validDigest),
				errText: "parse argon2id version",
			},
			{
				name:    "incompatible version",
				encoded: fmt.Sprintf("$argon2id$v=18$m=8192,t=1,p=1$%s$%s", validSalt, validDigest),
				errText: "incompatible argon2id version",
			},
			{
				name:    "invalid params syntax",
				encoded: fmt.Sprintf("$argon2id$v=19$m=bad,t=1,p=1$%s$%s", validSalt, validDigest),
				errText: "parse argon2id params",
			},
			{
				name:    "invalid base64 salt",
				encoded: fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$!!!invalid!!!$%s", validDigest),
				errText: "decode argon2id salt",
			},
			{
				name:    "invalid base64 hash",
				encoded: fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$!!!invalid!!!", validSalt),
				errText: "decode argon2id hash",
			},
			{
				name:    "empty decoded hash key",
				encoded: fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$", validSalt),
				errText: "empty argon2id hash key",
			},
		}

		for _, tc := range invalidCases {
			t.Run(tc.name, func(t *testing.T) {
				ok, verifyErr := hasher.Verify("Correta#2026", tc.encoded)
				assert.False(t, ok)
				require.Error(t, verifyErr)
				assert.Contains(t, verifyErr.Error(), tc.errText)
			})
		}
	})
}
