package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
)

// Erros comuns retornados pelos drivers de armazenamento de arquivos.
var (
	ErrUnsupportedMediaType = errors.New("formato de imagem não suportado (use JPEG, PNG ou WebP)")
	ErrInvalidObjectKey     = errors.New("chave de objeto inválida")
)

// Storage define o contrato para persistência e remoção de arquivos estáticos (disco local ou S3).
type Storage interface {
	Save(ctx context.Context, key, contentType string, data []byte) (string, error)
	Delete(ctx context.Context, key string) error
}

// DetectImageType inspeciona os magic bytes do payload e aceita apenas JPEG, PNG ou WebP.
func DetectImageType(data []byte) (contentType, ext string, ok bool) {
	if len(data) == 0 {
		return "", "", false
	}

	detected := http.DetectContentType(data)
	switch detected {
	case "image/jpeg":
		return "image/jpeg", ".jpg", true
	case "image/png":
		return "image/png", ".png", true
	case "image/webp":
		return "image/webp", ".webp", true
	default:
		return detected, "", false
	}
}

// BuildProductImageKey gera uma chave única e segura para evitar colisão e cache obsoleto de CDN.
func BuildProductImageKey(productID, ext string) (string, error) {
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("generate image key suffix: %w", err)
	}
	cleanID := strings.Trim(strings.ToLower(strings.TrimSpace(productID)), "/")
	if cleanID == "" || strings.Contains(cleanID, "..") || strings.ContainsAny(cleanID, `/\`) {
		return "", ErrInvalidObjectKey
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return fmt.Sprintf("products/%s-%s%s", cleanID, hex.EncodeToString(suffix), strings.ToLower(ext)), nil
}

// SanitizeKey valida e normaliza uma chave de objeto relativa contra path traversal.
func SanitizeKey(raw string) (string, error) {
	if raw == "" || strings.ContainsRune(raw, '\x00') || strings.ContainsRune(raw, '\\') {
		return "", ErrInvalidObjectKey
	}
	if strings.HasPrefix(raw, "/") {
		return "", ErrInvalidObjectKey
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", ErrInvalidObjectKey
	}
	return cleaned, nil
}
