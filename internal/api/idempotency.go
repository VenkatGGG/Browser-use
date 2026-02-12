package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/VenkatGGG/Browser-use/internal/idempotency"
	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

const idempotencyHeader = "Idempotency-Key"

func (s *Server) handleIdempotentRequest(w http.ResponseWriter, r *http.Request, scope string, execute func(http.ResponseWriter)) bool {
	if s.idempotency == nil {
		return false
	}

	key := strings.TrimSpace(r.Header.Get(idempotencyHeader))
	if key == "" {
		return false
	}

	if cached, ok, err := s.idempotency.Get(r.Context(), scope, key); err == nil && ok {
		writeIdempotencyEntry(w, cached)
		return true
	} else if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "idempotency_failed", err.Error())
		return true
	}

	owner := "idem-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	claimed, err := s.idempotency.Claim(r.Context(), scope, key, owner, s.idempotencyLock)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "idempotency_failed", err.Error())
		return true
	}
	if !claimed {
		if cached, ok, err := s.waitForIdempotentEntry(r.Context(), scope, key, 4*time.Second); err == nil && ok {
			writeIdempotencyEntry(w, cached)
			return true
		}
		httpx.WriteError(w, http.StatusConflict, "request_in_progress", "another request with this idempotency key is still in progress")
		return true
	}
	defer func() {
		_ = s.idempotency.Release(context.Background(), scope, key, owner)
	}()

	rec := httptest.NewRecorder()
	execute(rec)

	result := rec.Result()
	defer result.Body.Close()
	body, _ := io.ReadAll(result.Body)

	entry := idempotency.Entry{
		StatusCode:  result.StatusCode,
		ContentType: result.Header.Get("Content-Type"),
		Body:        bytes.Clone(body),
	}
	if result.StatusCode < 500 {
		_ = s.idempotency.Save(context.Background(), scope, key, entry, s.idempotencyTTL)
	}
	copyResponse(w, result.Header, result.StatusCode, body)
	return true
}

func (s *Server) waitForIdempotentEntry(ctx context.Context, scope, key string, timeout time.Duration) (idempotency.Entry, bool, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		entry, ok, err := s.idempotency.Get(waitCtx, scope, key)
		if err != nil {
			return idempotency.Entry{}, false, err
		}
		if ok {
			return entry, true, nil
		}

		select {
		case <-waitCtx.Done():
			return idempotency.Entry{}, false, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func writeIdempotencyEntry(w http.ResponseWriter, entry idempotency.Entry) {
	contentType := strings.TrimSpace(entry.ContentType)
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	status := entry.StatusCode
	if status <= 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(entry.Body)
}

func copyResponse(w http.ResponseWriter, header http.Header, status int, body []byte) {
	for key, values := range header {
		w.Header().Del(key)
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if status <= 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
