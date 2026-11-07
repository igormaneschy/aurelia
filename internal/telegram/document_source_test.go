package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTelegramDocumentSourceFetchesOriginalBytes(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(documentAPIStub{content: "PK\x00\xfforiginal", filePath: "documents/file.xlsx"})
	defer server.Close()
	source := newTelegramDocumentSource(server.URL, "test-token")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	body, err := source.Open(ctx, "file-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	content, err := io.ReadAll(body)
	if err != nil || string(content) != "PK\x00\xfforiginal" {
		t.Fatalf("download content = %q, error = %v", content, err)
	}
}

func TestTelegramDocumentSourceRejectsInvalidPathsAndAPIErrors(t *testing.T) {
	t.Parallel()
	for _, filePath := range []string{"../secret", "/absolute/file", "https://example.com/file", "documents/%2e%2e/secret", "documents/file?token=secret", "documents/file#fragment"} {
		t.Run(filePath, func(t *testing.T) {
			server := httptest.NewServer(documentAPIStub{filePath: filePath})
			defer server.Close()
			_, err := newTelegramDocumentSource(server.URL, "test-token").Open(context.Background(), "file-1")
			if err == nil || strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected safe rejection, got %v", err)
			}
		})
	}
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusTemporaryRedirect} {
		server := httptest.NewServer(documentAPIStub{status: status})
		_, err := newTelegramDocumentSource(server.URL, "test-token").Open(context.Background(), "file-1")
		server.Close()
		if err == nil || strings.Contains(err.Error(), "test-token") {
			t.Fatalf("HTTP %d: expected safe error, got %v", status, err)
		}
	}
}

func TestTelegramDocumentSourceHonorsCancellationWithoutLeakingToken(t *testing.T) {
	t.Parallel()
	source := newTelegramDocumentSource("http://127.0.0.1:1", "test-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := source.Open(ctx, "file-1")
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "test-token") {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestTelegramDocumentSourceRedactsTokenInMalformedRedirect(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(documentAPIStub{status: http.StatusFound, location: "/file/bottest-token/%zz"})
	defer server.Close()
	_, err := newTelegramDocumentSource(server.URL, "test-token").Open(context.Background(), "file-1")
	if err == nil || strings.Contains(err.Error(), "test-token") || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("malformed redirect error = %v", err)
	}
}

type documentAPIStub struct {
	content  string
	filePath string
	status   int
	location string
}

func (s documentAPIStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.status != 0 {
		location := s.location
		if location == "" {
			location = "http://127.0.0.1:1/should-not-follow"
		}
		w.Header().Set("Location", location)
		w.WriteHeader(s.status)
		return
	}
	if r.URL.Path == "/bottest-token/getFile" {
		var request struct {
			FileID string `json:"file_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.FileID != "file-1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "result": map[string]string{"file_path": s.filePath}})
		return
	}
	if r.URL.Path == "/file/bottest-token/documents/file.xlsx" {
		_, _ = io.WriteString(w, s.content)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}
