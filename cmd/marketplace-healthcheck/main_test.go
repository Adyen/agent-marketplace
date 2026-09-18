package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunReturnsNonZeroWhenReadinessFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var stderr bytes.Buffer
	if code := run(context.Background(), server.URL, &stderr); code != 1 {
		t.Fatalf("run() code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "503 Service Unavailable") {
		t.Fatalf("run() stderr = %q, want readiness failure", stderr.String())
	}
}

func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		wantErr    string
	}{
		{name: "ready", statusCode: http.StatusOK},
		{name: "not ready", statusCode: http.StatusServiceUnavailable, wantErr: "503 Service Unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			err := check(context.Background(), server.URL)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("check() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("check() error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
