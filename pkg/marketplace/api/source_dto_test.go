package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestSourceDTORemovesPrivateFieldsAndSanitizesOrigins(t *testing.T) {
	src := store.Source{
		ID:             "src-test",
		Slug:           "example",
		Aliases:        []string{"old-example"},
		Origins:        []string{"https://user:origin-secret@example.com/repo.git?token=query-secret&ref=main"},
		Ref:            "main",
		Roots:          []store.SourceRoot{{Path: ".agents"}},
		Acquisition:    store.AcquisitionMirror,
		CredentialsRef: "env:MARKETPLACE_GIT_TOKEN",
		PolicyOverrides: map[string]any{
			"internal": true,
		},
		RefreshStatus: store.SourceRefreshStatusFailed,
		RefreshError:  "clone failed with token=refresh-secret",
		RefreshedAt:   time.Unix(1, 0).UTC(),
		Listed:        true,
	}

	public := sourceDTO(src)
	data, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal source DTO: %v", err)
	}
	body := string(data)
	for _, secret := range []string{"origin-secret", "query-secret", "refresh-secret", "MARKETPLACE_GIT_TOKEN", "credentials_ref", "policy_overrides", "refresh_status", "refresh_error", "refreshed_at"} {
		if strings.Contains(body, secret) {
			t.Fatalf("source DTO leaked private value %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, "https://example.com/repo.git?ref=main\\u0026token=%5Bredacted%5D") {
		t.Fatalf("source DTO did not sanitize origin: %s", body)
	}
	if public.ID != src.ID || public.Slug != src.Slug || public.Ref != src.Ref || !public.Listed {
		t.Fatalf("source DTO omitted public metadata: %#v", public)
	}
}

func TestSourceDTOSanitizesOrOmitsUnsafePublisherURL(t *testing.T) {
	public := sourceDTO(store.Source{Publisher: store.Publisher{
		Name: "Example Publisher",
		URL:  "https://publisher.example/contact?oauth_token=publisher-secret#private",
	}})
	data, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal source DTO: %v", err)
	}
	body := string(data)
	if public.Publisher == nil || public.Publisher.Name != "Example Publisher" {
		t.Fatalf("source DTO omitted safe publisher name: %#v", public.Publisher)
	}
	if public.Publisher.URL != "" || strings.Contains(body, "publisher-secret") || strings.Contains(body, "private") {
		t.Fatalf("source DTO exposed unsafe publisher URL: %s", body)
	}
	if strings.Contains(body, `"url"`) {
		t.Fatalf("source DTO should omit unsafe publisher url: %s", body)
	}

	safe := sourceDTO(store.Source{Publisher: store.Publisher{URL: "https://publisher.example/contact"}})
	if safe.Publisher == nil || safe.Publisher.URL != "https://publisher.example/contact" {
		t.Fatalf("source DTO omitted safe publisher URL: %#v", safe.Publisher)
	}
}

func TestSourceDTOEmitsEmptyRootsArray(t *testing.T) {
	public := sourceDTO(store.Source{Origins: []string{}})
	if public.Roots == nil {
		t.Fatal("source DTO roots must be non-nil when empty")
	}
	body, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal source DTO: %v", err)
	}
	if !strings.Contains(string(body), `"roots":[]`) {
		t.Fatalf("source DTO must emit an empty roots array: %s", body)
	}
}

func TestSourceDTORedactsMalformedAndLegacyOrigins(t *testing.T) {
	src := store.Source{Origins: []string{
		"https://user:secret@example.com/repo.git",
		"https://example.com/repo.git?access_token=%zz",
		"https://user:secret@%zz",
		"git@github.com:private/repo.git",
	}}

	body, err := json.Marshal(sourceDTO(src))
	if err != nil {
		t.Fatalf("marshal source DTO: %v", err)
	}
	if strings.Contains(string(body), "secret") {
		t.Fatalf("source DTO leaked legacy origin credential: %s", body)
	}
	if !strings.Contains(string(body), "[redacted origin]") || strings.Contains(string(body), "user:") {
		t.Fatalf("source DTO did not fail closed for malformed/legacy origins: %s", body)
	}
}
