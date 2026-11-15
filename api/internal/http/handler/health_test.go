package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/domain"
	"github.com/joao-accioly/artisan-store/api/internal/http/handler"
	"github.com/joao-accioly/artisan-store/api/internal/http/oapi"
)

type stubPinger struct {
	pingFn func(ctx context.Context) error
}

func (s *stubPinger) Ping(ctx context.Context) error {
	return s.pingFn(ctx)
}

func TestServer_GetHealthz_Success(t *testing.T) {
	var receivedDeadline bool
	srv := handler.NewServer(&stubPinger{
		pingFn: func(ctx context.Context) error {
			_, receivedDeadline = ctx.Deadline()
			return nil
		},
	})

	before := time.Now().UTC().Add(-time.Second)
	resp, err := srv.GetHealthz(context.Background(), oapi.GetHealthzRequestObject{})
	after := time.Now().UTC().Add(time.Second)

	require.NoError(t, err)
	assert.True(t, receivedDeadline, "Ping must receive a context with timeout")

	okResp, ok := resp.(oapi.GetHealthz200JSONResponse)
	require.True(t, ok)
	assert.Equal(t, oapi.Ok, okResp.Status)
	assert.Equal(t, oapi.Up, okResp.Database)
	assert.Equal(t, time.UTC, okResp.Timestamp.Location())
	assert.True(t, okResp.Timestamp.After(before) && okResp.Timestamp.Before(after))

	rec := httptest.NewRecorder()
	require.NoError(t, okResp.VisitGetHealthzResponse(rec))
	assert.Equal(t, http.StatusOK, rec.Code)

	var decoded oapi.HealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &decoded))
	assert.Equal(t, oapi.Ok, decoded.Status)
	assert.Equal(t, oapi.Up, decoded.Database)
}

func TestServer_GetHealthz_NilDB(t *testing.T) {
	srv := handler.NewServer(nil)

	resp, err := srv.GetHealthz(context.Background(), oapi.GetHealthzRequestObject{})
	require.NoError(t, err)

	errResp, ok := resp.(oapi.GetHealthz503JSONResponse)
	require.True(t, ok)
	assert.Equal(t, string(domain.CodeServiceUnavailable), errResp.Error.Code)
	assert.Equal(t, "Banco de dados não inicializado", errResp.Error.Message)
	require.NotNil(t, errResp.Error.Details)
	assert.Equal(t, string(oapi.Down), (*errResp.Error.Details)["database"])

	rec := httptest.NewRecorder()
	require.NoError(t, errResp.VisitGetHealthzResponse(rec))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var decoded oapi.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &decoded))
	assert.Equal(t, string(domain.CodeServiceUnavailable), decoded.Error.Code)
}

func TestServer_GetHealthz_PingFailureAndCanceledContext(t *testing.T) {
	t.Run("ping returns error", func(t *testing.T) {
		srv := handler.NewServer(&stubPinger{
			pingFn: func(context.Context) error {
				return errors.New("connection reset by peer")
			},
		})

		resp, err := srv.GetHealthz(context.Background(), oapi.GetHealthzRequestObject{})
		require.NoError(t, err)

		errResp, ok := resp.(oapi.GetHealthz503JSONResponse)
		require.True(t, ok)
		assert.Equal(t, string(domain.CodeServiceUnavailable), errResp.Error.Code)
		assert.Equal(t, "Banco de dados indisponível", errResp.Error.Message)
		assert.NotContains(t, errResp.Error.Message, "connection reset by peer")
		require.NotNil(t, errResp.Error.Details)
		assert.Equal(t, string(oapi.Down), (*errResp.Error.Details)["database"])
	})

	t.Run("parent context canceled propagates to Ping", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		srv := handler.NewServer(&stubPinger{
			pingFn: func(pingCtx context.Context) error {
				return pingCtx.Err()
			},
		})

		resp, err := srv.GetHealthz(ctx, oapi.GetHealthzRequestObject{})
		require.NoError(t, err)

		_, ok := resp.(oapi.GetHealthz503JSONResponse)
		assert.True(t, ok)
	})
}
