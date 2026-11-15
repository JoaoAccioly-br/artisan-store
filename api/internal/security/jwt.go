package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
)

const jwtIssuer = "artisan-store-api"

// AccessClaims representa os dados assinados dentro do JWT de acesso.
type AccessClaims struct {
	UserID uuid.UUID   `json:"uid"`
	Email  string      `json:"email"`
	Role   domain.Role `json:"role"`
	jwt.RegisteredClaims
}

// JWTManager gera e valida tokens JWT de acesso e tokens opacos de refresh.
type JWTManager struct {
	secret    []byte
	accessTTL time.Duration
	nowFunc   func() time.Time
}

// NewJWTManager constrói um JWTManager usando segredo HMAC-SHA256 e TTL definidos.
func NewJWTManager(secret string, accessTTL time.Duration) (*JWTManager, error) {
	if len(secret) < 32 {
		return nil, errors.New("jwt secret must be at least 32 characters")
	}
	if accessTTL <= 0 {
		return nil, errors.New("jwt access TTL must be positive")
	}
	return &JWTManager{
		secret:    []byte(secret),
		accessTTL: accessTTL,
		nowFunc:   time.Now,
	}, nil
}

// GenerateAccessToken gera um JWT assinado para o usuário informado e retorna também o TTL em segundos.
func (m *JWTManager) GenerateAccessToken(user domain.User) (string, int, error) {
	now := m.nowFunc().UTC()
	expiresAt := now.Add(m.accessTTL)

	claims := AccessClaims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", 0, fmt.Errorf("sign access token: %w", err)
	}

	return signed, int(m.accessTTL.Seconds()), nil
}

// ParseAccessToken valida a assinatura, algoritmo, emissor e expiração de um JWT de acesso.
func (m *JWTManager) ParseAccessToken(tokenString string) (*AccessClaims, error) {
	var claims AccessClaims

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(jwtIssuer),
		jwt.WithTimeFunc(m.nowFunc),
	)

	token, err := parser.ParseWithClaims(tokenString, &claims, func(_ *jwt.Token) (any, error) {
		return m.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, domain.NewUnauthorizedError("Token de acesso inválido ou expirado")
	}

	if claims.UserID == uuid.Nil || !claims.Role.IsValid() {
		return nil, domain.NewUnauthorizedError("Token de acesso com claims inválidas")
	}

	return &claims, nil
}

// GenerateOpaqueRefreshToken cria um token aleatório seguro (em texto puro para o cliente) e seu hash SHA-256 (para o banco).
func GenerateOpaqueRefreshToken() (rawToken string, tokenHash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate refresh token bytes: %w", err)
	}

	raw := "rt_" + base64.RawURLEncoding.EncodeToString(buf)
	return raw, HashToken(raw), nil
}

// HashToken calcula o digest SHA-256 em hexadecimal de um token opaco.
func HashToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
