package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/joao-accioly/artisan-store/api/internal/http/middleware"
)

func TestRequestID_GeneratesAndPropagates(t *testing.T) {
	var ctxID string
	h := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = middleware.GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	headerID := rec.Header().Get(middleware.RequestIDHeader)
	assert.NotEmpty(t, headerID)
	assert.Equal(t, headerID, ctxID)
}

func TestRequestID_ReusesIncomingHeader(t *testing.T) {
	const incomingID = "client-req-abc-123"
	var ctxID string

	h := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = middleware.GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set(middleware.RequestIDHeader, incomingID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, incomingID, rec.Header().Get(middleware.RequestIDHeader))
	assert.Equal(t, incomingID, ctxID)
}

func TestRequestID_UniqueUnderConcurrency(t *testing.T) {
	const workers = 20
	h := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ids := make([]string, workers)
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			ids[idx] = rec.Header().Get(middleware.RequestIDHeader)
		}(i)
	}
	wg.Wait()

	seen := make(map[string]struct{}, workers)
	for _, id := range ids {
		require.NotEmpty(t, id)
		_, dup := seen[id]
		assert.False(t, dup, "duplicate request id: %s", id)
		seen[id] = struct{}{}
	}
}

func TestGetRequestID_EmptyContext(t *testing.T) {
	assert.Empty(t, middleware.GetRequestID(context.Background()))
}
