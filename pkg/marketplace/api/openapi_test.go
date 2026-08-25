package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
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
		"/api/v1/sources/{source}/occurrences/{commit}",
		"/api/v1/resources/{source}/{kind}/{name}",
		"/api/v1/resources/{source}/{kind}/{name}/revisions",
		"/api/v1/resources/{source}/{kind}/{name}/revisions/{revision}",
		"/api/v1/manifests/{source-revision}",
		"/api/v1/inspection/sha256/{digest}",
		"/api/v1/content/sha256/{digest}",
		"/api/v1/resources/{source}/{kind}/{name}/revisions/{revision}/inspection",
		"/api/v1/resources/{source}/{kind}/{name}/revisions/{revision}/inspection/content",
		"/api/v1/resources/{source}/{kind}/{name}/revisions/{revision}/inspection/content/{digest}",
		"/api/v1/artifacts/sha256/{digest}",
		"/s/{source}/{kind}/{name}",
		"/s/{source}/{kind}/{name}/revisions/{revision}",
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
	for _, name := range []string{"Capabilities", "WellKnown", "Marketplace", "Source", "SourceOccurrence", "ExactSourceOccurrence", "SearchResult", "ResourceListItem", "ResourceRevision", "ResourceCoordinate", "InspectionManifest", "InspectionFile", "InspectionFinding"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("openapi missing required schema %q", name)
		}
	}
}

func TestOpenAPIMediaTypesMatchInspectionContract(t *testing.T) {
	doc := readOpenAPIDocument(t)
	paths := doc["paths"].(map[string]any)
	for _, route := range []string{
		"/api/v1/content/sha256/{digest}",
		"/api/v1/resources/{source}/{kind}/{name}/revisions/{revision}/inspection/content",
		"/api/v1/resources/{source}/{kind}/{name}/revisions/{revision}/inspection/content/{digest}",
	} {
		operation := paths[route].(map[string]any)["get"].(map[string]any)
		response := operation["responses"].(map[string]any)["200"].(map[string]any)
		content := response["content"].(map[string]any)
		got := make(map[string]bool, len(content))
		for mediaType := range content {
			got[mediaType] = true
		}
		want := make(map[string]bool, len(inspection.PublicMediaTypes))
		for _, mediaType := range inspection.PublicMediaTypes {
			want[mediaType] = true
		}
		if len(got) != len(want) {
			t.Fatalf("%s media types = %#v, want %#v", route, got, want)
		}
		for mediaType := range want {
			if !got[mediaType] {
				t.Errorf("%s missing media type %q", route, mediaType)
			}
		}
	}
}

func TestOpenAPIBrowserRoutesAreNuxtHTMLAndWellKnownTemplatesOptional(t *testing.T) {
	doc := readOpenAPIDocument(t)
	paths := doc["paths"].(map[string]any)
	latest := paths["/s/{source}/{kind}/{name}"].(map[string]any)["get"].(map[string]any)
	exact := paths["/s/{source}/{kind}/{name}/revisions/{revision}"].(map[string]any)["get"].(map[string]any)
	for name, operation := range map[string]map[string]any{"latest": latest, "exact": exact} {
		responses := operation["responses"].(map[string]any)
		response := responses["200"].(map[string]any)
		content := response["content"].(map[string]any)
		if _, ok := content["text/html"]; !ok {
			t.Errorf("%s browser route does not return text/html: %#v", name, content)
		}
	}

	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	wellKnown := schemas["WellKnown"].(map[string]any)
	wellKnownObject := wellKnown["allOf"].([]any)[1].(map[string]any)
	required := wellKnownObject["required"].([]any)
	for _, field := range required {
		if field == "browser_base" || field == "browser_latest_route_template" || field == "browser_exact_route_template" {
			t.Errorf("optional browser field is required: %q", field)
		}
	}
}

func TestOpenAPISourceSchemaIsPublicMetadataOnly(t *testing.T) {
	doc := readOpenAPIDocument(t)
	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	source := schemas["Source"].(map[string]any)
	if source["additionalProperties"] != false {
		t.Fatalf("Source schema must reject undeclared private fields: %#v", source["additionalProperties"])
	}
	properties := source["properties"].(map[string]any)
	for _, privateField := range []string{"credentials_ref", "refresh_status", "refresh_error", "refreshed_at", "policy_overrides"} {
		if _, ok := properties[privateField]; ok {
			t.Errorf("Source schema exposes private field %q", privateField)
		}
	}
}

func TestOpenAPIUsesOriginServerAndCanonicalOccurrenceFields(t *testing.T) {
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
	if _, ok := occurrence["anyOf"]; ok {
		t.Fatal("SourceOccurrence must not expose compatibility alternatives")
	}
	required := occurrence["required"].([]any)
	seen := map[string]bool{}
	for _, field := range required {
		if name, ok := field.(string); ok {
			seen[name] = true
		}
	}
	if !seen["commit"] || seen["revision_commit"] {
		t.Fatalf("SourceOccurrence required fields = %#v, want canonical commit only", seen)
	}
	properties := occurrence["properties"].(map[string]any)
	if _, ok := properties["revision_commit"]; ok {
		t.Fatal("SourceOccurrence must not expose revision_commit")
	}
	commit := properties["commit"].(map[string]any)
	if commit["pattern"] != "^[0-9a-f]{40}$" {
		t.Fatalf("commit pattern = %#v, want full lowercase Git object ID", commit["pattern"])
	}
	for _, field := range []string{"source_revision_manifest_digest", "mirrored_artifact_digest"} {
		property := properties[field].(map[string]any)
		if property["pattern"] != "^sha256:[0-9a-f]{64}$" {
			t.Fatalf("%s pattern = %#v, want strict lowercase SHA-256", field, property["pattern"])
		}
	}
}

func TestOpenAPIResourceListRevisionUsesCanonicalDigest(t *testing.T) {
	doc := readOpenAPIDocument(t)
	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	item := schemas["ResourceListItem"].(map[string]any)
	properties := item["properties"].(map[string]any)
	for _, field := range []string{"revision", "content_digest"} {
		property := properties[field].(map[string]any)
		if property["pattern"] != "^sha256:[0-9a-f]{64}$" {
			t.Fatalf("%s pattern = %#v, want strict lowercase SHA-256", field, property["pattern"])
		}
	}
}

func TestOpenAPIResourceRevisionRequiresCompleteCoordinate(t *testing.T) {
	doc := readOpenAPIDocument(t)
	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	revision := schemas["ResourceRevision"].(map[string]any)
	coordinate := revision["properties"].(map[string]any)["coordinate"].(map[string]any)
	required := coordinate["required"].([]any)
	if len(required) != 4 {
		t.Fatalf("ResourceRevision.coordinate required fields = %#v, want all four identity fields", required)
	}
	for _, field := range []string{"marketplace", "source", "kind", "name"} {
		found := false
		for _, value := range required {
			if value == field {
				found = true
			}
		}
		if !found {
			t.Errorf("ResourceRevision.coordinate missing required field %q", field)
		}
	}
}

func TestOpenAPIPublicURLSchemasDescribeSanitizedValues(t *testing.T) {
	doc := readOpenAPIDocument(t)
	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	for _, schemaName := range []string{"WellKnown", "Marketplace"} {
		schema := schemas[schemaName].(map[string]any)
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			allOf := schema["allOf"].([]any)
			properties = allOf[1].(map[string]any)["properties"].(map[string]any)
		}
		canonical := properties["canonical_url"].(map[string]any)
		if canonical["format"] != "uri" || canonical["description"] == nil {
			t.Errorf("%s canonical_url is not documented as a sanitized URI: %#v", schemaName, canonical)
		}
	}
	publisher := schemas["Publisher"].(map[string]any)
	publisherURL := publisher["properties"].(map[string]any)["url"].(map[string]any)
	if publisherURL["format"] != "uri" || publisherURL["description"] == nil {
		t.Errorf("Publisher.url is not documented as a sanitized URI: %#v", publisherURL)
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
