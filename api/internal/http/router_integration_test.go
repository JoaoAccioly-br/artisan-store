package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/joao-accioly/artisan-store/api/internal/config"
	"github.com/joao-accioly/artisan-store/api/internal/domain"
	apphttp "github.com/joao-accioly/artisan-store/api/internal/http"
	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
	"github.com/joao-accioly/artisan-store/api/internal/platform/db"
	"github.com/joao-accioly/artisan-store/api/internal/platform/logger"
	"github.com/joao-accioly/artisan-store/api/internal/repository/sqlc"
)

func setupPostgresContainer(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("doceria_test"),
		postgres.WithUsername("doceria"),
		postgres.WithPassword("doceria_test_password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, pgContainer.Terminate(context.Background()))
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	return connStr
}

func TestRouterAndDatabaseIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("pulando teste de integração com Docker no modo -short")
	}

	ctx := context.Background()
	connStr := setupPostgresContainer(t)

	pool, err := db.NewPool(ctx, connStr)
	require.NoError(t, err)
	defer pool.Close()

	// Valida migrations up, down e up novamente sem erro.
	require.NoError(t, db.MigrateUp(ctx, pool))
	require.NoError(t, db.MigrateDown(ctx, pool))
	require.NoError(t, db.MigrateUp(ctx, pool))

	// Valida query gerada pelo sqlc contra o banco migrado.
	queries := sqlc.New(pool)
	pingVal, err := queries.Ping(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(1), pingVal)

	var logBuf bytes.Buffer
	log := logger.NewWithWriter(&logBuf, "development", "debug")
	cfg := &config.Config{
		AppEnv:         "development",
		AllowedOrigins: []string{"http://localhost:3000"},
		RateLimitRPM:   10,
	}

	router := apphttp.NewRouter(cfg, log, apphttp.RouterDeps{DB: pool})

	t.Run("GET /healthz retorna 200 com X-Request-Id e CORS", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotEmpty(t, rec.Header().Get(middleware.RequestIDHeader))
		assert.Equal(t, "http://localhost:3000", rec.Header().Get("Access-Control-Allow-Origin"))

		var body oapi.HealthResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, oapi.Ok, body.Status)
		assert.Equal(t, oapi.Up, body.Database)
		assert.False(t, body.Timestamp.IsZero())
	})

	t.Run("rota inexistente retorna 404 no padrão ErrorResponse", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nao-existe", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.NotEmpty(t, rec.Header().Get(middleware.RequestIDHeader))
		var body oapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, string(domain.CodeNotFound), body.Error.Code)
	})

	t.Run("método não permitido retorna 405 no padrão ErrorResponse", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
		assert.NotEmpty(t, rec.Header().Get(middleware.RequestIDHeader))
		var body oapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, string(domain.CodeMethodNotAllowed), body.Error.Code)
		assert.Equal(t, "Método HTTP não permitido para esta rota", body.Error.Message)
	})

	t.Run("rate limit retorna 429 no padrão ErrorResponse", func(t *testing.T) {
		cfgStrict := &config.Config{
			AppEnv:         "development",
			AllowedOrigins: []string{"http://localhost:3000"},
			RateLimitRPM:   2,
		}
		limitedRouter := apphttp.NewRouter(cfgStrict, log, apphttp.RouterDeps{DB: pool})

		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			req.RemoteAddr = "192.0.2.10:12345"
			rec := httptest.NewRecorder()
			limitedRouter.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
		}

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "192.0.2.10:12345"
		rec := httptest.NewRecorder()
		limitedRouter.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusTooManyRequests, rec.Code)
		var body oapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, string(domain.CodeRateLimited), body.Error.Code)
	})

	t.Run("GET /healthz retorna 503 quando o pool é fechado", func(t *testing.T) {
		pool.Close()

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		var body oapi.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, string(domain.CodeServiceUnavailable), body.Error.Code)
	})
}
