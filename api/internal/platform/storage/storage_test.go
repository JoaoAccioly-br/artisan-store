package storage_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/platform/storage"
)

func validJPEGBytes() []byte {
	return []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00}
}

func validPNGBytes() []byte {
	return []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R'}
}

func validWebPBytes() []byte {
	return []byte{'R', 'I', 'F', 'F', 0x10, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '}
}

type mockS3Client struct {
	putFn    func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	deleteFn func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

func (m *mockS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.putFn != nil {
		return m.putFn(ctx, params, optFns...)
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, params, optFns...)
	}
	return &s3.DeleteObjectOutput{}, nil
}

func TestDetectImageType(t *testing.T) {
	t.Run("accepts valid JPEG, PNG and WebP magic bytes", func(t *testing.T) {
		tests := []struct {
			name     string
			data     []byte
			wantType string
			wantExt  string
		}{
			{name: "jpeg", data: validJPEGBytes(), wantType: "image/jpeg", wantExt: ".jpg"},
			{name: "png", data: validPNGBytes(), wantType: "image/png", wantExt: ".png"},
			{name: "webp", data: validWebPBytes(), wantType: "image/webp", wantExt: ".webp"},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				ct, ext, ok := storage.DetectImageType(tc.data)
				assert.True(t, ok)
				assert.Equal(t, tc.wantType, ct)
				assert.Equal(t, tc.wantExt, ext)
			})
		}
	})

	t.Run("rejects unsafe or unknown payloads", func(t *testing.T) {
		tests := []struct {
			name string
			data []byte
		}{
			{name: "empty slice", data: []byte{}},
			{name: "nil slice", data: nil},
			{name: "svg payload", data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
			{name: "html payload", data: []byte(`<!DOCTYPE html><html><body>hello</body></html>`)},
			{name: "plain text disguised as jpg", data: []byte("fake image content pretending to be .jpg")},
			{name: "gif89a unsupported", data: []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00")},
			{name: "truncated jpeg header (< min bytes)", data: []byte{0xFF, 0xD8}},
			{name: "truncated webp header (< 12 bytes)", data: []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B'}},
			{name: "unknown binary bytes", data: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				_, ext, ok := storage.DetectImageType(tc.data)
				assert.False(t, ok)
				assert.Empty(t, ext)
			})
		}
	})
}

func TestSanitizeKeyAndBuildProductImageKey(t *testing.T) {
	t.Run("SanitizeKey accepts valid relative keys and cleans safe segments", func(t *testing.T) {
		got, err := storage.SanitizeKey("products/brigadeiro-123.jpg")
		require.NoError(t, err)
		assert.Equal(t, "products/brigadeiro-123.jpg", got)

		got, err = storage.SanitizeKey("products/./sub/img.webp")
		require.NoError(t, err)
		assert.Equal(t, "products/sub/img.webp", got)

		got, err = storage.SanitizeKey("products/img.png/")
		require.NoError(t, err)
		assert.Equal(t, "products/img.png", got)
	})

	t.Run("SanitizeKey rejects traversal, null byte, backslash and leading slash", func(t *testing.T) {
		invalidKeys := []string{
			"",
			".",
			"..",
			"../secret.txt",
			"products/../../etc/passwd",
			"a/../b/../../c.jpg",
			"/products/image.jpg",
			"/",
			"products\\image.jpg",
			"products/image\x00.jpg",
		}

		for _, key := range invalidKeys {
			_, err := storage.SanitizeKey(key)
			assert.ErrorIs(t, err, storage.ErrInvalidObjectKey, "key=%q", key)
		}
	})

	t.Run("BuildProductImageKey formats and validates product IDs", func(t *testing.T) {
		key1, err := storage.BuildProductImageKey("  /01930000-ABCD-7000-8000-000000000001/ ", "PNG")
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(key1, "products/01930000-abcd-7000-8000-000000000001-"))
		assert.True(t, strings.HasSuffix(key1, ".png"))

		key2, err := storage.BuildProductImageKey("01930000-abcd-7000-8000-000000000001", ".webp")
		require.NoError(t, err)
		assert.NotEqual(t, key1, key2)
		assert.True(t, strings.HasSuffix(key2, ".webp"))

		for _, badID := range []string{"", "   ", "/", "../etc", "a/b", "a\\b", "foo..bar"} {
			_, badErr := storage.BuildProductImageKey(badID, ".jpg")
			assert.ErrorIs(t, badErr, storage.ErrInvalidObjectKey, "productID=%q", badID)
		}
	})
}

func TestLocalStorage(t *testing.T) {
	t.Run("NewLocalStorage validates baseDir", func(t *testing.T) {
		for _, dir := range []string{"", "   "} {
			st, err := storage.NewLocalStorage(dir, "http://localhost:8080/uploads")
			assert.Nil(t, st)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "base directory is required")
		}
	})

	t.Run("Save atomic, overwrite, PublicURL, Delete idempotent and traversal protection", func(t *testing.T) {
		baseDir := t.TempDir()
		st, err := storage.NewLocalStorage(baseDir, "http://localhost:8080/uploads/")
		require.NoError(t, err)

		ctx := context.Background()
		url1, err := st.Save(ctx, "products/bolo.png", "image/png", validPNGBytes())
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:8080/uploads/products/bolo.png", url1)

		// Verifica conteúdo gravado e ausência de arquivos temporários residuais.
		diskPath := filepath.Join(baseDir, "products", "bolo.png")
		gotBytes, err := os.ReadFile(diskPath)
		require.NoError(t, err)
		assert.Equal(t, validPNGBytes(), gotBytes)

		entries, err := os.ReadDir(filepath.Join(baseDir, "products"))
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "bolo.png", entries[0].Name())

		// Overwrite atômico na mesma chave.
		newData := validJPEGBytes()
		url2, err := st.Save(ctx, "products/bolo.png", "image/jpeg", newData)
		require.NoError(t, err)
		assert.Equal(t, url1, url2)

		gotBytes, err = os.ReadFile(diskPath)
		require.NoError(t, err)
		assert.Equal(t, newData, gotBytes)

		// Delete remove arquivo e segunda chamada é idempotente.
		require.NoError(t, st.Delete(ctx, "products/bolo.png"))
		_, statErr := os.Stat(diskPath)
		assert.True(t, os.IsNotExist(statErr))
		require.NoError(t, st.Delete(ctx, "products/bolo.png"))

		// Traversal é bloqueado em Save e Delete.
		_, err = st.Save(ctx, "../outside.jpg", "image/jpeg", validJPEGBytes())
		assert.ErrorIs(t, err, storage.ErrInvalidObjectKey)
		err = st.Delete(ctx, "../outside.jpg")
		assert.ErrorIs(t, err, storage.ErrInvalidObjectKey)

		// Contexto cancelado retorna erro.
		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()
		_, err = st.Save(canceledCtx, "products/a.jpg", "image/jpeg", validJPEGBytes())
		assert.ErrorIs(t, err, context.Canceled)
		err = st.Delete(canceledCtx, "products/a.jpg")
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("Save returns relative URL when publicBaseURL is empty", func(t *testing.T) {
		st, err := storage.NewLocalStorage(t.TempDir(), "")
		require.NoError(t, err)

		u, err := st.Save(context.Background(), "products/brig.webp", "image/webp", validWebPBytes())
		require.NoError(t, err)
		assert.Equal(t, "/products/brig.webp", u)
	})

	t.Run("FileServerHandler serves files securely with headers and blocks traversal/methods/dirs", func(t *testing.T) {
		baseDir := t.TempDir()
		st, err := storage.NewLocalStorage(baseDir, "http://localhost:8080/uploads")
		require.NoError(t, err)

		pngData := validPNGBytes()
		_, err = st.Save(context.Background(), "products/doce.png", "image/png", pngData)
		require.NoError(t, err)

		h := st.FileServerHandler("/uploads")

		// GET válido
		req := httptest.NewRequest(http.MethodGet, "/uploads/products/doce.png", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		assert.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
		assert.Equal(t, pngData, rec.Body.Bytes())

		// HEAD válido
		reqHead := httptest.NewRequest(http.MethodHead, "/uploads/products/doce.png", nil)
		recHead := httptest.NewRecorder()
		h.ServeHTTP(recHead, reqHead)
		assert.Equal(t, http.StatusOK, recHead.Code)
		assert.Equal(t, "nosniff", recHead.Header().Get("X-Content-Type-Options"))

		// Diretórios e arquivos inexistentes -> 404
		for _, p := range []string{"/uploads/", "/uploads/products", "/uploads/products/", "/uploads/products/nao-existe.png"} {
			r := httptest.NewRequest(http.MethodGet, p, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			assert.Equal(t, http.StatusNotFound, w.Code, "path=%s", p)
		}

		// Path traversal -> 404
		for _, p := range []string{
			"/uploads/../secret.txt",
			"/uploads/products/../../etc/passwd",
			"/uploads/products\\doce.png",
		} {
			r := httptest.NewRequest(http.MethodGet, p, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			assert.Equal(t, http.StatusNotFound, w.Code, "traversal path=%s", p)
		}

		// Métodos não permitidos -> 405
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			r := httptest.NewRequest(method, "/uploads/products/doce.png", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code, "method=%s", method)
		}
	})
}

func TestS3Storage(t *testing.T) {
	t.Run("NewS3Storage validates required config and endpoint URL", func(t *testing.T) {
		ctx := context.Background()

		_, err := storage.NewS3Storage(ctx, storage.S3Config{Region: "us-east-1", AccessKey: "k", SecretKey: "s"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bucket is required")

		_, err = storage.NewS3Storage(ctx, storage.S3Config{Bucket: "b", AccessKey: "k", SecretKey: "s"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "region is required")

		_, err = storage.NewS3Storage(ctx, storage.S3Config{Bucket: "b", Region: "us-east-1", AccessKey: "k"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "credentials are required")

		_, err = storage.NewS3Storage(ctx, storage.S3Config{
			Bucket:    "b",
			Region:    "us-east-1",
			AccessKey: "k",
			SecretKey: "s",
			Endpoint:  "not-a-valid-url",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid s3 endpoint")
	})

	t.Run("Save and Delete via mock s3ObjectAPI and PublicURL formatting", func(t *testing.T) {
		var capturedPut *s3.PutObjectInput
		var capturedBody []byte
		var capturedDelete *s3.DeleteObjectInput

		client := &mockS3Client{
			putFn: func(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
				capturedPut = params
				var readErr error
				capturedBody, readErr = io.ReadAll(params.Body)
				require.NoError(t, readErr)
				return &s3.PutObjectOutput{}, nil
			},
			deleteFn: func(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
				capturedDelete = params
				return &s3.DeleteObjectOutput{}, nil
			},
		}

		payload := validWebPBytes()

		// 1. Com PublicBaseURL configurada (CDN / domínio próprio)
		stPublic := storage.NewS3StorageWithClient(client, storage.S3Config{
			Bucket:        "doceria-bucket",
			Region:        "sa-east-1",
			Endpoint:      "http://localhost:9000",
			PublicBaseURL: "https://cdn.doceria.com/",
			UsePathStyle:  true,
		})

		u, err := stPublic.Save(context.Background(), "products/torta.webp", "image/webp", payload)
		require.NoError(t, err)
		assert.Equal(t, "https://cdn.doceria.com/products/torta.webp", u)
		require.NotNil(t, capturedPut)
		assert.Equal(t, "doceria-bucket", aws.ToString(capturedPut.Bucket))
		assert.Equal(t, "products/torta.webp", aws.ToString(capturedPut.Key))
		assert.Equal(t, "image/webp", aws.ToString(capturedPut.ContentType))
		assert.Equal(t, "public, max-age=31536000, immutable", aws.ToString(capturedPut.CacheControl))
		assert.Equal(t, payload, capturedBody)

		require.NoError(t, stPublic.Delete(context.Background(), "products/torta.webp"))
		require.NotNil(t, capturedDelete)
		assert.Equal(t, "doceria-bucket", aws.ToString(capturedDelete.Bucket))
		assert.Equal(t, "products/torta.webp", aws.ToString(capturedDelete.Key))

		// 2. Path-style com custom endpoint (sem PublicBaseURL)
		stPathStyle := storage.NewS3StorageWithClient(client, storage.S3Config{
			Bucket:       "doceria-bucket",
			Region:       "us-east-1",
			Endpoint:     "http://localhost:9000/",
			UsePathStyle: true,
		})
		u, err = stPathStyle.Save(context.Background(), "products/torta.webp", "image/webp", payload)
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:9000/doceria-bucket/products/torta.webp", u)

		// 3. Virtual-hosted com custom endpoint
		stCustomVHost := storage.NewS3StorageWithClient(client, storage.S3Config{
			Bucket:       "doceria-bucket",
			Region:       "auto",
			Endpoint:     "https://r2.example.com",
			UsePathStyle: false,
		})
		u, err = stCustomVHost.Save(context.Background(), "products/torta.webp", "image/webp", payload)
		require.NoError(t, err)
		assert.Equal(t, "https://doceria-bucket.r2.example.com/products/torta.webp", u)

		// 4. URL padrão AWS virtual-hosted (sem endpoint e sem PublicBaseURL)
		stAWS := storage.NewS3StorageWithClient(client, storage.S3Config{
			Bucket: "doceria-bucket",
			Region: "sa-east-1",
		})
		u, err = stAWS.Save(context.Background(), "products/torta.webp", "image/webp", payload)
		require.NoError(t, err)
		assert.Equal(t, "https://doceria-bucket.s3.sa-east-1.amazonaws.com/products/torta.webp", u)
	})

	t.Run("S3Storage error handling and invalid keys", func(t *testing.T) {
		uninit := storage.NewS3StorageWithClient(nil, storage.S3Config{Bucket: "b", Region: "us-east-1"})
		_, err := uninit.Save(context.Background(), "products/a.jpg", "image/jpeg", validJPEGBytes())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not initialized")
		err = uninit.Delete(context.Background(), "products/a.jpg")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not initialized")

		failingClient := &mockS3Client{
			putFn: func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
				return nil, errors.New("s3 put boom")
			},
			deleteFn: func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
				return nil, errors.New("s3 delete boom")
			},
		}
		st := storage.NewS3StorageWithClient(failingClient, storage.S3Config{Bucket: "b", Region: "us-east-1"})

		_, err = st.Save(context.Background(), "../bad.jpg", "image/jpeg", validJPEGBytes())
		assert.ErrorIs(t, err, storage.ErrInvalidObjectKey)

		err = st.Delete(context.Background(), "../bad.jpg")
		assert.ErrorIs(t, err, storage.ErrInvalidObjectKey)

		_, err = st.Save(context.Background(), "products/a.jpg", "image/jpeg", validJPEGBytes())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "put s3 object")

		err = st.Delete(context.Background(), "products/a.jpg")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delete s3 object")
	})

	t.Run("NewS3Storage real SDK wire call against httptest.Server", func(t *testing.T) {
		var methods []string
		var paths []string
		var putContentType string
		var putCacheControl string
		var putBody []byte

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			methods = append(methods, r.Method)
			paths = append(paths, r.URL.Path)
			if r.Method == http.MethodPut {
				putContentType = r.Header.Get("Content-Type")
				putCacheControl = r.Header.Get("Cache-Control")
				var readErr error
				putBody, readErr = io.ReadAll(r.Body)
				require.NoError(t, readErr)
				w.Header().Set("ETag", `"mock-etag"`)
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
		}))
		defer srv.Close()

		st, err := storage.NewS3Storage(context.Background(), storage.S3Config{
			Endpoint:     srv.URL,
			Region:       "us-east-1",
			Bucket:       "doceria-test",
			AccessKey:    "test-access",
			SecretKey:    "test-secret",
			UsePathStyle: true,
		})
		require.NoError(t, err)

		payload := validJPEGBytes()
		u, err := st.Save(context.Background(), "products/item.jpg", "image/jpeg", payload)
		require.NoError(t, err)
		assert.Equal(t, srv.URL+"/doceria-test/products/item.jpg", u)
		assert.Equal(t, "image/jpeg", putContentType)
		assert.Equal(t, "public, max-age=31536000, immutable", putCacheControl)
		assert.Equal(t, payload, putBody)

		require.NoError(t, st.Delete(context.Background(), "products/item.jpg"))
		assert.Equal(t, []string{http.MethodPut, http.MethodDelete}, methods)
		assert.Equal(t, []string{"/doceria-test/products/item.jpg", "/doceria-test/products/item.jpg"}, paths)
	})
}
