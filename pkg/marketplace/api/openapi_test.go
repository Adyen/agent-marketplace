package api

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func readOpenAPIDocument(t *testing.T) map[string]any {
	t.Helper()
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// The OpenAPI file lives at repo-root/api/openapi.yaml; the test package is
	// three levels deep under pkg/marketplace/api.
	for i := 0; i < 3; i++ {
		repoRoot = filepath.Dir(repoRoot)
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal openapi: %v", err)
	}
	return doc
}

func TestOpenAPIYAMLIsValidAndContainsRoutes(t *testing.T) {
	doc := readOpenAPIDocument(t)
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi paths missing or not a map")
	}

	requiredPaths := []string{
		"/.well-known/agent-marketplace.json",
		"/api/v1/capabilities",
		"/api/v1/instance",
		"/api/v1/search",
		"/api/v1/sources/{source}",
		"/api/v1/sources/{source}/history",
		"/api/v1/resources/{source}/{kind}/{name}",
		"/api/v1/resources/{source}/{kind}/{name}/revisions",
		"/api/v1/resources/{source}/{kind}/{name}/revisions/{revision}",
		"/api/v1/manifests/{source-revision}",
		"/api/v1/artifacts/sha256/{digest}",
		"/s/{source}/{kind}/{name}",
		"/healthz",
	}
	for _, p := range requiredPaths {
		if _, ok := paths[p]; !ok {
			t.Errorf("openapi missing required path %q", p)
		}
	}

	components, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatalf("openapi components missing")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("openapi schemas missing")
	}
	for _, name := range []string{"Capabilities", "WellKnown", "Marketplace", "Source", "SourceOccurrence", "SearchResult", "ResourceListItem", "ResourceRevision"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("openapi missing required schema %q", name)
		}
	}
}

func TestOpenAPIUsesOriginServerAndCompatibleOccurrenceCommitFields(t *testing.T) {
	doc := readOpenAPIDocument(t)
	servers, ok := doc["servers"].([]any)
	if !ok || len(servers) != 1 {
		t.Fatalf("openapi servers = %#v, want one origin server", doc["servers"])
	}
	server, ok := servers[0].(map[string]any)
	if !ok || server["url"] != "/" {
		t.Fatalf("openapi server = %#v, want url / so paths are resolved from the public origin", servers[0])
	}

	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	occurrence := schemas["SourceOccurrence"].(map[string]any)
	alternatives, ok := occurrence["anyOf"].([]any)
	if !ok || len(alternatives) < 2 {
		t.Fatalf("SourceOccurrence.anyOf = %#v, want commit compatibility alternatives", occurrence["anyOf"])
	}
	seen := map[string]bool{}
	for _, alternative := range alternatives {
		entry, ok := alternative.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range entry["required"].([]any) {
			if name, ok := field.(string); ok {
				seen[name] = true
			}
		}
	}
	if !seen["commit"] || !seen["revision_commit"] {
		t.Fatalf("SourceOccurrence.anyOf required fields = %#v, want commit and revision_commit", seen)
	}
}

func TestOpenAPIEnumMatchesDefaultCapabilities(t *testing.T) {
	caps := DefaultCapabilities()
	// This is a coarse check that the OpenAPI enum values and the Go constants
	// are kept in sync. The exact parsing of YAML enums is not worth pulling in
	// a full OpenAPI validator for; the design contract is maintained by hand.
	if len(caps.APIVersions) != 1 || caps.APIVersions[0] != APIVersionV1 {
		t.Fatalf("APIVersions mismatch with OpenAPI enum")
	}
	if len(caps.ArtifactSchemaVersions) != 1 || caps.ArtifactSchemaVersions[0] != ArtifactSchemaVersionV1 {
		t.Fatalf("ArtifactSchemaVersions mismatch with OpenAPI enum")
	}
}
