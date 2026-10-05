package api

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
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

// spec: marketplace-api/openapi-document-declares-the-marketplace-paths-and-schemas
func TestOpenAPIYAMLIsValidAndContainsRoutes(t *testing.T) {
	doc := readOpenAPIDocument(t)
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi paths missing or not a map")
	}

	requiredPaths := []string{
		"/.well-known/agent-marketplace.json",
		"/api/v1/capabilities",
		"/api/v1/catalog",
		"/api/v1/instance",
		"/api/v1/search",
		"/api/v1/registries",
		"/api/v1/registries/{registry}",
		"/api/v1/registries/{registry}/history",
		"/api/v1/registries/{registry}/occurrences/{commit}",
		"/api/v1/resources/{registry}/{kind}/{name}",
		"/api/v1/resources/{registry}/{kind}/{name}/revisions",
		"/api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}",
		"/api/v1/manifests/{registry-revision}",
		"/api/v1/assessments/sha256/{digest}",
		"/api/v1/inspection/sha256/{digest}",
		"/api/v1/content/sha256/{digest}",
		"/api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection",
		"/api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection/content",
		"/api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection/content/{digest}",
		"/api/v1/artifacts/sha256/{digest}",
		"/r/{registry}/{kind}/{name}",
		"/r/{registry}/{kind}/{name}/revisions/{revision}",
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
	for _, name := range []string{"Capabilities", "WellKnown", "Marketplace", "Registry", "RegistryOccurrence", "ExactRegistryOccurrence", "SearchResult", "ResourceListItem", "ResourceRevision", "ResourceAssessment", "AssessmentFinding", "ResourceOccurrence", "Revocation", "ResourceCoordinate", "InspectionManifest", "InspectionFile", "CatalogKeysetCursor", "CatalogETag", "CatalogIfNoneMatch", "CatalogPage", "CatalogResource", "CatalogSource", "CatalogCollection", "CatalogCollectionResourceReference", "CatalogCollectionIncludeReference", "CatalogUnresolvedReference"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("openapi missing required schema %q", name)
		}
	}
}

// spec: marketplace-admin-auth/published-contract-matches-browser-and-json-behavior
func TestOpenAPIAdminAuthDocumentsBrowserAndJSONErrors(t *testing.T) {
	paths := readOpenAPIDocument(t)["paths"].(map[string]any)
	wantErrors := map[string]map[string][]string{
		"/api/v1/admin/auth/login": {
			"400": {"authentication_request_invalid"},
			"403": {"origin_rejected"},
			"503": {"authentication_unavailable", "admin_unavailable"},
		},
		"/api/v1/admin/auth/callback": {
			"400": {"authentication_request_invalid"},
			"401": {"authentication_rejected"},
			"403": {"forbidden", "origin_rejected"},
			"503": {"authentication_unavailable", "admin_unavailable"},
		},
	}
	for path, statuses := range wantErrors {
		operation := paths[path].(map[string]any)["get"].(map[string]any)
		responses := operation["responses"].(map[string]any)
		redirect := responses["303"].(map[string]any)["description"].(string)
		if !strings.Contains(redirect, "/marketplace/auth/error") {
			t.Errorf("%s 303 description omits the browser error page: %q", path, redirect)
		}
		var acceptFound bool
		for _, parameter := range operation["parameters"].([]any) {
			p := parameter.(map[string]any)
			if p["name"] == "Accept" && p["in"] == "header" {
				acceptFound = true
			}
		}
		if !acceptFound {
			t.Errorf("%s does not document the Accept negotiation header", path)
		}
		for status, codes := range statuses {
			response, ok := responses[status].(map[string]any)
			if !ok {
				t.Errorf("%s %s error response missing", path, status)
				continue
			}
			description, _ := response["description"].(string)
			for _, code := range codes {
				if !strings.Contains(description, code) {
					t.Errorf("%s %s description omits code %q: %q", path, status, code, description)
				}
			}
			media, ok := response["content"].(map[string]any)["application/json"]
			if !ok {
				t.Errorf("%s %s lacks an application/json error body", path, status)
				continue
			}
			schema, ok := media.(map[string]any)["schema"].(map[string]any)
			if !ok || schema["type"] != "object" {
				t.Errorf("%s %s error schema is not an object: %v", path, status, media)
				continue
			}
			properties, _ := schema["properties"].(map[string]any)
			for _, field := range []string{"code", "message"} {
				property, ok := properties[field].(map[string]any)
				if !ok || property["type"] != "string" {
					t.Errorf("%s %s error schema property %q is not a string schema: %v", path, status, field, properties[field])
				}
				required, _ := schema["required"].([]any)
				if !slices.ContainsFunc(required, func(item any) bool { return item == field }) {
					t.Errorf("%s %s error schema does not require %q", path, status, field)
				}
			}
		}
	}
}

// spec: marketplace-api/capabilities-document-matches-the-openapi-contract
func TestDefaultCapabilitiesMatchOpenAPIEnums(t *testing.T) {
	caps := DefaultCapabilities()
	if !slices.Equal(caps.APIVersions, []string{"v1"}) {
		t.Errorf("APIVersions = %v, want [v1]", caps.APIVersions)
	}
	if !slices.Equal(caps.ArtifactSchemaVersions, []string{"v1"}) {
		t.Errorf("ArtifactSchemaVersions = %v, want [v1]", caps.ArtifactSchemaVersions)
	}
	properties := readOpenAPIDocument(t)["components"].(map[string]any)["schemas"].(map[string]any)["Capabilities"].(map[string]any)["properties"].(map[string]any)
	enum := properties["resource_kinds"].(map[string]any)["items"].(map[string]any)["enum"].([]any)
	if !sameAnyStrings(enum, caps.ResourceKinds) {
		t.Errorf("Capabilities.resource_kinds enum = %v, DefaultCapabilities().ResourceKinds = %v", enum, caps.ResourceKinds)
	}
}

func TestOpenAPIDiscoveryRequiresRunningVersion(t *testing.T) {
	doc := readOpenAPIDocument(t)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	wellKnown := schemas["WellKnown"].(map[string]any)["allOf"].([]any)[1].(map[string]any)
	assertSchemaRequiredFields(t, "WellKnown", wellKnown, []string{"instance_id", "version", "canonical_url", "api_base"})
	version := wellKnown["properties"].(map[string]any)["version"].(map[string]any)
	if version["type"] != "string" || version["description"] == nil {
		t.Fatalf("WellKnown.version = %#v, want documented string", version)
	}

	marketplace := schemas["Marketplace"].(map[string]any)
	assertSchemaRequiredFields(t, "Marketplace", marketplace, []string{"id", "name", "canonical_url", "version"})
	version = marketplace["properties"].(map[string]any)["version"].(map[string]any)
	if version["type"] != "string" || version["description"] == nil {
		t.Fatalf("Marketplace.version = %#v, want documented string", version)
	}
}

func TestOpenAPICatalogProjectionContract(t *testing.T) {
	doc := readOpenAPIDocument(t)
	paths := doc["paths"].(map[string]any)
	operation := paths["/api/v1/catalog"].(map[string]any)["get"].(map[string]any)

	params := operation["parameters"].([]any)
	gotParams := map[string]map[string]any{}
	for _, raw := range params {
		param := raw.(map[string]any)
		gotParams[param["in"].(string)+":"+param["name"].(string)] = param
	}
	for _, name := range []string{"query:after", "query:limit", "header:If-None-Match"} {
		if _, ok := gotParams[name]; !ok {
			t.Errorf("catalog endpoint missing %s parameter", name)
		}
	}
	limit := gotParams["query:limit"]["schema"].(map[string]any)
	if limit["minimum"] != 1 || limit["maximum"] != 500 || limit["default"] != 500 {
		t.Errorf("catalog limit = %#v, want bounds 1..500 and default 500", limit)
	}
	if ref := gotParams["query:after"]["schema"].(map[string]any)["$ref"]; ref != "#/components/schemas/CatalogKeysetCursor" {
		t.Errorf("catalog after schema ref = %#v", ref)
	}
	if ref := gotParams["header:If-None-Match"]["schema"].(map[string]any)["$ref"]; ref != "#/components/schemas/CatalogIfNoneMatch" {
		t.Errorf("catalog If-None-Match schema ref = %#v", ref)
	}

	responses := operation["responses"].(map[string]any)
	for _, status := range []string{"200", "304"} {
		response, ok := responses[status].(map[string]any)
		if !ok {
			t.Errorf("catalog endpoint missing %s response", status)
			continue
		}
		headers := response["headers"].(map[string]any)
		if ref := headers["ETag"].(map[string]any)["schema"].(map[string]any)["$ref"]; ref != "#/components/schemas/CatalogETag" {
			t.Errorf("catalog %s ETag ref = %#v", status, ref)
		}
		cache := headers["Cache-Control"].(map[string]any)["schema"].(map[string]any)["enum"].([]any)
		if len(cache) != 1 || cache[0] != "no-cache" {
			t.Errorf("catalog %s Cache-Control = %#v, want no-cache", status, cache)
		}
	}
	if _, ok := responses["304"].(map[string]any)["content"]; ok {
		t.Error("catalog 304 response must have no body")
	}
	if ref := responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]; ref != "#/components/schemas/CatalogPage" {
		t.Errorf("catalog 200 schema ref = %#v", ref)
	}

	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	page := schemas["CatalogPage"].(map[string]any)
	assertSchemaRequiredFields(t, "CatalogPage", page, []string{"api_version", "marketplace_id", "catalog_version", "complete", "resources"})
	if page["additionalProperties"] != false {
		t.Errorf("CatalogPage additionalProperties = %#v, want false", page["additionalProperties"])
	}
	resource := schemas["CatalogResource"].(map[string]any)
	assertSchemaRequiredFields(t, "CatalogResource", resource, []string{"registry_id", "registry_slug", "kind", "name", "revision", "availability"})
	properties := resource["properties"].(map[string]any)
	if _, hasType := properties["type"]; hasType {
		t.Error("CatalogResource must retain kind rather than introduce type")
	}
	if values := properties["kind"].(map[string]any)["enum"].([]any); !sameAnyStrings(values, []string{"skill", "command", "subagent", "hook", "mcpserver", "collection"}) {
		t.Errorf("catalog kind enum = %#v", values)
	}
	if values := properties["availability"].(map[string]any)["enum"].([]any); !sameAnyStrings(values, []string{"available", "operator-exposed", "blocked", "revoked"}) {
		t.Errorf("catalog availability enum = %#v", values)
	}
	for _, schemaName := range []string{"CatalogSource", "CatalogCollection", "CatalogCollectionResourceReference", "CatalogCollectionIncludeReference", "CatalogUnresolvedReference"} {
		if schemas[schemaName].(map[string]any)["additionalProperties"] != false {
			t.Errorf("%s additionalProperties must be false", schemaName)
		}
	}
	assertSchemaRequiredFields(t, "CatalogSource", schemas["CatalogSource"].(map[string]any), []string{"repository_url", "path", "commit"})
	assertSchemaRequiredFields(t, "CatalogCollection", schemas["CatalogCollection"].(map[string]any), []string{"resources", "includes", "unresolved_references"})
	assertSchemaRequiredFields(t, "CatalogCollectionResourceReference", schemas["CatalogCollectionResourceReference"].(map[string]any), []string{"registry_id", "kind", "name"})
	assertSchemaRequiredFields(t, "CatalogCollectionIncludeReference", schemas["CatalogCollectionIncludeReference"].(map[string]any), []string{"registry_id", "kind", "name"})
	assertSchemaRequiredFields(t, "CatalogUnresolvedReference", schemas["CatalogUnresolvedReference"].(map[string]any), []string{"reference_type", "kind", "name", "reason"})
	if values := schemas["CatalogCollectionIncludeReference"].(map[string]any)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]any); !sameAnyStrings(values, []string{"collection"}) {
		t.Errorf("catalog collection include kind enum = %#v, want collection only", values)
	}
	if values := schemas["CatalogUnresolvedReference"].(map[string]any)["properties"].(map[string]any)["reference_type"].(map[string]any)["enum"].([]any); !sameAnyStrings(values, []string{"resource", "include"}) {
		t.Errorf("catalog unresolved reference types = %#v", values)
	}
	if schema := schemas["CatalogETag"].(map[string]any); schema["pattern"] != `^"[^"]+"$` {
		t.Errorf("catalog ETag schema = %#v, want strong quoted validator", schema)
	}
	if schema := schemas["CatalogIfNoneMatch"].(map[string]any); schema["description"] == nil {
		t.Error("catalog If-None-Match schema must document HTTP list and wildcard syntax")
	}
}

func assertSchemaRequiredFields(t *testing.T, schemaName string, schema map[string]any, want []string) {
	t.Helper()
	if got := requiredFields(schema); !slices.Equal(got, want) {
		t.Errorf("%s required fields = %#v, want %#v", schemaName, got, want)
	}
}

func sameAnyStrings(raw []any, want []string) bool {
	if len(raw) != len(want) {
		return false
	}
	for index, value := range raw {
		if value != want[index] {
			return false
		}
	}
	return true
}

// spec: marketplace-api/inspection-content-routes-serve-the-public-media-types
func TestOpenAPIMediaTypesMatchInspectionContract(t *testing.T) {
	doc := readOpenAPIDocument(t)
	paths := doc["paths"].(map[string]any)
	for _, route := range []string{
		"/api/v1/content/sha256/{digest}",
		"/api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection/content",
		"/api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection/content/{digest}",
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

// spec: marketplace-api/well-known-document-exposes-browser-templates-only-when-configured
func TestOpenAPIBrowserRoutesAreNuxtHTMLAndWellKnownTemplatesOptional(t *testing.T) {
	doc := readOpenAPIDocument(t)
	paths := doc["paths"].(map[string]any)
	latest := paths["/r/{registry}/{kind}/{name}"].(map[string]any)["get"].(map[string]any)
	exact := paths["/r/{registry}/{kind}/{name}/revisions/{revision}"].(map[string]any)["get"].(map[string]any)
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

func TestOpenAPIRegistrySchemaIsPublicMetadataOnly(t *testing.T) {
	doc := readOpenAPIDocument(t)
	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	source := schemas["Registry"].(map[string]any)
	if source["additionalProperties"] != false {
		t.Fatalf("Registry schema must reject undeclared private fields: %#v", source["additionalProperties"])
	}
	properties := source["properties"].(map[string]any)
	for _, privateField := range []string{"credentials_ref", "refresh_status", "refresh_error", "refreshed_at", "policy_overrides"} {
		if _, ok := properties[privateField]; ok {
			t.Errorf("Registry schema exposes private field %q", privateField)
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
	occurrence := schemas["RegistryOccurrence"].(map[string]any)
	if _, ok := occurrence["anyOf"]; ok {
		t.Fatal("RegistryOccurrence must not expose compatibility alternatives")
	}
	required := occurrence["required"].([]any)
	seen := map[string]bool{}
	for _, field := range required {
		if name, ok := field.(string); ok {
			seen[name] = true
		}
	}
	if !seen["commit"] || seen["revision_commit"] {
		t.Fatalf("RegistryOccurrence required fields = %#v, want canonical commit only", seen)
	}
	properties := occurrence["properties"].(map[string]any)
	if _, ok := properties["revision_commit"]; ok {
		t.Fatal("RegistryOccurrence must not expose revision_commit")
	}
	commit := properties["commit"].(map[string]any)
	if commit["pattern"] != "^[0-9a-f]{40}$" {
		t.Fatalf("commit pattern = %#v, want full lowercase Git object ID", commit["pattern"])
	}
	for _, field := range []string{"registry_revision_manifest_digest", "mirrored_artifact_digest"} {
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

// The description is untrusted third-party content, so the schema must publish
// the same bound the server enforces and must not make the field required.
func TestOpenAPIDescriptionIsOptionalAndBounded(t *testing.T) {
	doc := readOpenAPIDocument(t)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)

	for _, schemaName := range []string{"ResourceListItem", "ResourceRevision"} {
		schema := schemas[schemaName].(map[string]any)
		properties := schema["properties"].(map[string]any)

		description, ok := properties["description"].(map[string]any)
		if !ok {
			t.Fatalf("%s does not declare a description property", schemaName)
		}
		if description["type"] != "string" {
			t.Errorf("%s.description type = %#v, want string", schemaName, description["type"])
		}
		if description["maxLength"] != store.MaxDescriptionBytes {
			t.Errorf("%s.description maxLength = %#v, want %d (the bound the server enforces)",
				schemaName, description["maxLength"], store.MaxDescriptionBytes)
		}
		if description["description"] == nil {
			t.Errorf("%s.description is not documented as sanitized untrusted content", schemaName)
		}
		for _, field := range requiredFields(schema) {
			if field == "description" {
				t.Errorf("%s.description must stay optional: a resource kind may declare none", schemaName)
			}
		}
	}
}

// Availability is additive: it must be optional, and its enum must match the
// Go constants exactly so a client can switch on the documented values.
func TestOpenAPIAvailabilityMatchesServerConstants(t *testing.T) {
	doc := readOpenAPIDocument(t)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	item := schemas["ResourceListItem"].(map[string]any)

	availability, ok := item["properties"].(map[string]any)["availability"].(map[string]any)
	if !ok {
		t.Fatal("ResourceListItem does not declare an availability property")
	}
	if availability["type"] != "string" {
		t.Errorf("availability type = %#v, want string", availability["type"])
	}
	if availability["description"] == nil {
		t.Error("availability is not documented")
	}

	rawEnum, ok := availability["enum"].([]any)
	if !ok {
		t.Fatalf("availability enum = %#v, want a list", availability["enum"])
	}
	got := make(map[string]bool, len(rawEnum))
	for _, value := range rawEnum {
		got[value.(string)] = true
	}
	want := map[string]bool{
		string(AvailabilityAvailable):       true,
		string(AvailabilityBlocked):         true,
		string(AvailabilityPolicyBlocked):   true,
		string(AvailabilityOperatorExposed): true,
		string(AvailabilityNoArtifact):      true,
		string(AvailabilityRevoked):         true,
	}
	if len(got) != len(want) {
		t.Fatalf("availability enum = %#v, want %#v", got, want)
	}
	for value := range want {
		if !got[value] {
			t.Errorf("availability enum missing %q", value)
		}
	}

	for _, field := range requiredFields(item) {
		if field == "availability" {
			t.Error("availability must stay optional so existing clients remain conformant")
		}
	}
}

func requiredFields(schema map[string]any) []string {
	raw, ok := schema["required"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if name, ok := value.(string); ok {
			out = append(out, name)
		}
	}
	return out
}

func TestOpenAPIResourceRevisionRequiresCompleteCoordinate(t *testing.T) {
	doc := readOpenAPIDocument(t)
	components := doc["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	revision := schemas["ResourceRevision"].(map[string]any)
	coordinateRef := revision["properties"].(map[string]any)["coordinate"].(map[string]any)
	if coordinateRef["$ref"] != "#/components/schemas/ResourceCoordinate" {
		t.Fatalf("ResourceRevision.coordinate ref = %#v", coordinateRef["$ref"])
	}
	coordinate := schemas["ResourceCoordinate"].(map[string]any)
	required := coordinate["required"].([]any)
	if len(required) != 4 {
		t.Fatalf("ResourceRevision.coordinate required fields = %#v, want all four identity fields", required)
	}
	for _, field := range []string{"marketplace", "registry", "kind", "name"} {
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
