package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LocalStorage armazena arquivos no sistema de arquivos local para desenvolvimento ou deploy em VPS única.
type LocalStorage struct {
	baseDir       string
	publicBaseURL string
}

// NewLocalStorage inicializa o driver em disco garantindo que o diretório base exista.
func NewLocalStorage(baseDir, publicBaseURL string) (*LocalStorage, error) {
	trimmedDir := strings.TrimSpace(baseDir)
	if trimmedDir == "" {
		return nil, errors.New("local storage base directory is required")
	}

	absDir, err := filepath.Abs(trimmedDir)
	if err != nil {
		return nil, fmt.Errorf("resolve local storage directory: %w", err)
	}

	if err := os.MkdirAll(absDir, 0o750); err != nil {
		return nil, fmt.Errorf("create local storage directory: %w", err)
	}

	return &LocalStorage{
		baseDir:       absDir,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
	}, nil
}

// Save grava os bytes de forma atômica em disco e retorna a URL pública final.
func (s *LocalStorage) Save(ctx context.Context, key, _ string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	targetPath, cleanKey, err := s.resolvePath(key)
	if err != nil {
		return "", err
	}

	targetDir := filepath.Dir(targetPath)
	if mkErr := os.MkdirAll(targetDir, 0o750); mkErr != nil {
		return "", fmt.Errorf("create target directory: %w", mkErr)
	}

	tmpFile, err := os.CreateTemp(targetDir, ".upload-*")
	if err != nil {
		return "", fmt.Errorf("create temp upload file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			if remErr := os.Remove(tmpPath); remErr != nil && !errors.Is(remErr, fs.ErrNotExist) {
				return
			}
		}
	}()

	if _, writeErr := tmpFile.Write(data); writeErr != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			return "", fmt.Errorf("write upload data (%w) and close temp file: %w", writeErr, closeErr)
		}
		return "", fmt.Errorf("write upload data: %w", writeErr)
	}
	if chmodErr := tmpFile.Chmod(0o644); chmodErr != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			return "", fmt.Errorf("set permissions (%w) and close temp file: %w", chmodErr, closeErr)
		}
		return "", fmt.Errorf("set upload file permissions: %w", chmodErr)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		return "", fmt.Errorf("close temp upload file: %w", closeErr)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		return "", fmt.Errorf("commit uploaded file: %w", err)
	}
	cleanupTmp = false

	if s.publicBaseURL == "" {
		return "/" + cleanKey, nil
	}
	return s.publicBaseURL + "/" + cleanKey, nil
}

// Delete remove um objeto do disco ignorando erros quando o arquivo já não existir.
func (s *LocalStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	targetPath, _, err := s.resolvePath(key)
	if err != nil {
		return err
	}

	if err := os.Remove(targetPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete local object: %w", err)
	}
	return nil
}

// FileServerHandler retorna um http.Handler seguro para servir os uploads locais sem listagem de diretórios.
func (s *LocalStorage) FileServerHandler(urlPrefix string) http.Handler {
	cleanPrefix := "/" + strings.Trim(urlPrefix, "/")
	if cleanPrefix != "/" {
		cleanPrefix += "/"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		rel := strings.TrimPrefix(r.URL.Path, cleanPrefix)
		targetPath, _, err := s.resolvePath(path.Clean(rel))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		info, err := os.Stat(targetPath)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, targetPath)
	})
}

func (s *LocalStorage) resolvePath(rawKey string) (absTarget, cleanKey string, err error) {
	cleanKey, err = SanitizeKey(rawKey)
	if err != nil {
		return "", "", err
	}

	candidate := filepath.Join(s.baseDir, filepath.FromSlash(cleanKey))
	rel, err := filepath.Rel(s.baseDir, candidate)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", "", ErrInvalidObjectKey
	}
	return candidate, cleanKey, nil
}
