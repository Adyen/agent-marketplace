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

type coordinateFailureMetadata struct {
	store.MetadataStore
}

func (coordinateFailureMetadata) GetRegistryBySlug(context.Context, string) (store.Registry, error) {
	return store.Registry{ID: "registry"}, nil
}

func (coordinateFailureMetadata) GetLatestResourceRevision(context.Context, store.ResourceCoordinate) (store.AssessedResourceRevision, error) {
	return store.AssessedResourceRevision{}, errors.New("store failure")
}

type occurrenceFailureMetadata struct {
	store.MetadataStore
}

func (occurrenceFailureMetadata) GetRegistryBySlug(context.Context, string) (store.Registry, error) {
	return store.Registry{ID: "registry\r\nforged"}, nil
}

func (occurrenceFailureMetadata) GetRegistryOccurrence(context.Context, store.RegistryID, string) (store.RegistryOccurrence, error) {
	return store.RegistryOccurrence{}, errors.New("store failure")
}

func captureLogAttributes(t *testing.T) map[string]string {
	t.Helper()
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	attributes := map[string]string{}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			attributes[attr.Key] = attr.Value.String()
			return attr
		},
	})))
	return attributes
}

func TestRegistryFailureEncodesLogAttributes(t *testing.T) {
	attributes := captureLogAttributes(t)
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

// spec: configuration-settings/untrusted-log-values-cannot-create-records
func TestResourceFailureEncodesCoordinateLogAttribute(t *testing.T) {
	attributes := captureLogAttributes(t)
	server := &Server{mpID: "mp", meta: coordinateFailureMetadata{}}
	response := httptest.NewRecorder()
	server.handleResourceDetail(response, httptest.NewRequest(http.MethodGet, "/api/v1/resources/registry/skill/example", nil), "registry", "skill", "name\r\nforged")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("response = %d, want 500", response.Code)
	}
	if attributes["coord"] != `mp/registry/skill/name\r\nforged` || attributes["err"] != "store failure" {
		t.Fatalf("unencoded coordinate attributes: %#v", attributes)
	}
}

func TestRegistryOccurrenceFailureEncodesStoredRegistryID(t *testing.T) {
	attributes := captureLogAttributes(t)
	server := &Server{meta: occurrenceFailureMetadata{}}
	response := httptest.NewRecorder()
	commit := "0123456789abcdef0123456789abcdef01234567"
	server.handleRegistryOccurrence(response, httptest.NewRequest(http.MethodGet, "/api/v1/registries/example/occurrences/"+commit, nil), "example", commit)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("response = %d, want 500", response.Code)
	}
	if attributes["registry"] != `registry\r\nforged` || attributes["commit"] != commit {
		t.Fatalf("unencoded registry attributes: %#v", attributes)
	}
}
