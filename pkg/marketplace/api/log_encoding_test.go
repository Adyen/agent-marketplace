package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type logFailureMetadata struct {
	store.MetadataStore
}

func (logFailureMetadata) GetRegistryBySlug(context.Context, string) (store.Registry, error) {
	return store.Registry{}, errors.New("store failure\r\nforged record")
}

func TestRegistryFailureEncodesLogAttributes(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	attributes := map[string]string{}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			attributes[attr.Key] = attr.Value.String()
			return attr
		},
	})))
	server := &Server{meta: logFailureMetadata{}}
	response := httptest.NewRecorder()
	server.handleRegistryDetail(response, httptest.NewRequest(http.MethodGet, "/api/v1/registries/example", nil), "registry\r\nforged")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("response = %d, want 500", response.Code)
	}
	if attributes["registry"] != `registry\r\nforged` || attributes["err"] != `store failure\r\nforged record` {
		t.Fatalf("unencoded failure attributes: %#v", attributes)
	}
}
