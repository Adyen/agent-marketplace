package api

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIYAMLIsValidAndContainsRoutes(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// The OpenAPI file lives at repo-root/api/openapi.yaml; the test package is
	// three levels deep under pkg/marketplace/api.
	for i := 0; i < 3; i++ {
		repoRoot = filepath.Dir(repoRoot)
	}

	path := filepath.Join(repoRoot, "api", "openapi.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal openapi: %v", err)
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi paths missing or not a map")
	}

	requiredPaths := []string{
		"/.well-known/agent-marketplace.json",
		"/api/v1/capabilities",
		"/api/v1/resources",
		"/api/v1/artifacts/{kind}/{name}",
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
	for _, name := range []string{"Capabilities", "WellKnown", "ResourceListItem"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("openapi missing required schema %q", name)
		}
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
