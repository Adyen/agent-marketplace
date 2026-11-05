package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"gopkg.in/yaml.v3"
)

const geminiAPIDocsURL = "https://ai.google.dev/gemini-api/docs/models"

// spec: model-catalog-audit/gemini-api-page-inventory-lists-a-model-absent-from-the-catalog
func TestGeminiAPIDocsExtractsListItemIdentifiers(t *testing.T) {
	body, err := os.ReadFile("testdata/gemini-api-models.html")
	if err != nil {
		t.Fatal(err)
	}
	got, err := extract(source{Name: "gemini-api-models", Harness: "gemini", URL: geminiAPIDocsURL, Kind: "gemini-docs"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 44 {
		t.Fatalf("Gemini API model count = %d, want 44", len(got.Models))
	}
	if !slices.ContainsFunc(got.Models, func(model observedModel) bool { return model.ID == "gemini-4-argon" }) {
		t.Fatalf("Gemini API inventory does not include gemini-4-argon: %#v", got.Models)
	}
}

// spec: model-catalog-audit/gemini-api-page-inventory-lists-a-model-absent-from-the-catalog
func TestGeminiAPIDocsSourceIsRegisteredAndTrusted(t *testing.T) {
	var found bool
	for _, source := range sources {
		if source.Name == "gemini-api-models" && source.Harness == "gemini" && source.URL == geminiAPIDocsURL && source.Kind == "gemini-docs" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Gemini API model documentation source is not registered")
	}
	if !trustedURL(geminiAPIDocsURL) {
		t.Fatalf("Gemini API model documentation URL is not trusted: %s", geminiAPIDocsURL)
	}
}

// spec: model-catalog-audit/gemini-api-page-inventory-lists-a-model-absent-from-the-catalog
// spec: model-catalog-audit/gemini-api-page-inventory-lists-a-model-present-in-the-catalog
func TestGeminiAPIAuditReportsCatalogCoverage(t *testing.T) {
	body, err := os.ReadFile("testdata/gemini-api-models.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name        string
		catalogHas  bool
		wantMissing bool
	}{
		{name: "model missing", wantMissing: true},
		{name: "model present", catalogHas: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			models := "{}"
			if tt.catalogHas {
				models = "{google/gemini-4-argon: {model: gemini-4-argon}}"
			}
			catalogBody := fmt.Sprintf("schema: 1\nharness: gemini\nevidence: {refs: [%s]}\nsurfaces: {subagent: {model_field: model}}\nmodels: %s\n", geminiAPIDocsURL, models)
			files := fstest.MapFS{catalogPath("gemini"): &fstest.MapFile{Data: []byte(catalogBody)}}
			s := source{Name: "gemini-api-models", Harness: "gemini", URL: geminiAPIDocsURL, Kind: "gemini-docs"}
			result := audit(context.Background(), files, []source{s}, nil, func(context.Context, string) (fetched, error) {
				return fetched{Body: body, URL: s.URL}, nil
			}, time.Now())
			if len(result.Sources) != 1 {
				t.Fatalf("sources = %#v", result.Sources)
			}
			if !slices.ContainsFunc(result.Sources[0].Observed.Models, func(model observedModel) bool { return model.ID == "gemini-4-argon" }) {
				t.Fatalf("audit did not retain gemini-4-argon from the page: %#v", result.Sources[0].Observed.Models)
			}
			missing := slices.ContainsFunc(result.Sources[0].Findings, func(f finding) bool {
				return f.Code == "catalog.model-missing" && f.Model == "gemini-4-argon"
			})
			if missing != tt.wantMissing {
				t.Fatalf("catalog.model-missing for gemini-4-argon = %t, want %t; findings: %#v", missing, tt.wantMissing, result.Sources[0].Findings)
			}
		})
	}
}

// spec: model-catalog-audit/gemini-api-page-without-identifiers-fails-closed
func TestGeminiAPIDocsWithoutIdentifiersIsUnknown(t *testing.T) {
	catalogBody := fmt.Sprintf("schema: 1\nharness: gemini\nevidence: {refs: [%s]}\nsurfaces: {subagent: {model_field: model}}\nmodels: {}\n", geminiAPIDocsURL)
	files := fstest.MapFS{catalogPath("gemini"): &fstest.MapFile{Data: []byte(catalogBody)}}
	s := source{Name: "gemini-api-models", Harness: "gemini", URL: geminiAPIDocsURL, Kind: "gemini-docs"}
	result := audit(context.Background(), files, []source{s}, nil, func(context.Context, string) (fetched, error) {
		return fetched{Body: []byte("<ul><li>Gemini model name</li></ul>"), URL: s.URL}, nil
	}, time.Now())
	if len(result.Sources) != 1 || result.Sources[0].Name != s.Name || result.Sources[0].Status != "unknown" {
		t.Fatalf("empty Gemini model inventory status = %#v", result.Sources)
	}
	if len(result.Sources[0].Findings) != 1 || result.Sources[0].Findings[0].Code != "source.inventory-unparsed" {
		t.Fatalf("empty Gemini model inventory findings = %#v", result.Sources[0].Findings)
	}
}

// spec: model-catalog-audit/factory-haiku-pin-trails-the-observed-haiku
// spec: model-catalog-audit/family-pin-is-the-newest-observed-member
func TestFactoryHaikuFamilyPinStaleness(t *testing.T) {
	catalogBody, err := os.ReadFile("../../pkg/harness-compilation/modelpolicy/catalogs/factory.yaml")
	if err != nil {
		t.Fatal(err)
	}
	currentCatalog := bytes.Replace(catalogBody, []byte("pin: anthropic/claude-haiku-4-5-20251001"), []byte("pin: anthropic/claude-haiku-5-5"), 1)
	if bytes.Equal(currentCatalog, catalogBody) {
		t.Fatal("Factory Haiku family pin was not found")
	}
	body := "| Model | Model ID | Multiplier | Reasoning |\n| --- | --- | --- | --- |\n| Haiku 4.5 | `claude-haiku-4-5-20251001` | 1 | `low` |\n| Haiku 5.5 | `claude-haiku-5-5` | 1 | `low` |"
	s := source{Name: "factory-models", Harness: "factory", URL: "https://docs.factory.ai/models.md", Kind: "factory"}
	for _, tt := range []struct {
		name        string
		catalogBody []byte
		wantStale   bool
	}{
		{name: "observed newer model", catalogBody: catalogBody, wantStale: true},
		{name: "current pin", catalogBody: currentCatalog},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := fstest.MapFS{catalogPath("factory"): &fstest.MapFile{Data: bytes.Clone(tt.catalogBody)}}
			before := bytes.Clone(files[catalogPath("factory")].Data)
			result := audit(context.Background(), files, []source{s}, nil, func(context.Context, string) (fetched, error) {
				return fetched{Body: []byte(body), URL: s.URL}, nil
			}, time.Now())
			stale := slices.ContainsFunc(result.Sources[0].Findings, func(f finding) bool {
				return f.Code == "catalog.family-pin-stale" && f.Model == "anthropic/haiku"
			})
			if stale != tt.wantStale {
				t.Fatalf("catalog.family-pin-stale = %t, want %t", stale, tt.wantStale)
			}
			if !bytes.Equal(files[catalogPath("factory")].Data, before) {
				t.Fatal("audit changed the Factory catalog")
			}
		})
	}
}

// spec: model-catalog-audit/family-pin-is-the-newest-observed-member
func TestFamilyPinVersionsCompareNumericComponents(t *testing.T) {
	catalogBody := []byte(`schema: 1
harness: factory
evidence: {refs: [https://docs.factory.ai/models.md]}
surfaces: {subagent: {model_field: model}}
models:
  anthropic/claude-haiku-4-9: {model: claude-haiku-4-9}
  anthropic/claude-haiku-4-9-0: {model: claude-haiku-4-9-0}
  anthropic/claude-haiku-4-10: {model: claude-haiku-4-10}
families:
  anthropic/haiku: {pin: anthropic/claude-haiku-4-9}
`)
	body := "| Model | Model ID | Multiplier | Reasoning |\n| --- | --- | --- | --- |\n| Haiku 4.9 | `claude-haiku-4-9` | 1 | `low` |\n| Haiku 4.9.0 | `claude-haiku-4-9-0` | 1 | `low` |\n| Haiku 4.10 | `claude-haiku-4-10` | 1 | `low` |"
	s := source{Name: "factory-models", Harness: "factory", URL: "https://docs.factory.ai/models.md", Kind: "factory"}
	for _, tt := range []struct {
		name      string
		observed  string
		wantStale bool
	}{
		{name: "numeric components", observed: body, wantStale: true},
		{name: "missing trailing component is zero", observed: strings.ReplaceAll(body, "| Haiku 4.10 | `claude-haiku-4-10` | 1 | `low` |", "| Other | `claude-haiku-4-9-0` | 1 | `low` |")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := fstest.MapFS{catalogPath("factory"): &fstest.MapFile{Data: catalogBody}}
			result := audit(context.Background(), files, []source{s}, nil, func(context.Context, string) (fetched, error) {
				return fetched{Body: []byte(tt.observed), URL: s.URL}, nil
			}, time.Now())
			stale := slices.ContainsFunc(result.Sources[0].Findings, func(f finding) bool {
				return f.Code == "catalog.family-pin-stale" && f.Model == "anthropic/haiku"
			})
			if stale != tt.wantStale {
				t.Fatalf("catalog.family-pin-stale = %t, want %t", stale, tt.wantStale)
			}
		})
	}
}

// spec: model-catalog-audit/every-non-fusion-snapshot-uid-is-covered
// spec: model-catalog-audit/non-fusion-snapshot-uid-is-not-covered
func TestDevinSnapshotUIDsAreCoveredByCatalog(t *testing.T) {
	body, err := os.ReadFile("testdata/devin-models.json")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Families []struct {
			FamilyUID string `json:"family_uid"`
			Variants  []struct {
				ModelUID string `json:"model_uid"`
			} `json:"variants"`
		} `json:"families"`
	}
	if err := json.Unmarshal(body, &snapshot); err != nil {
		t.Fatal(err)
	}
	var uids []string
	for _, family := range snapshot.Families {
		for _, variant := range family.Variants {
			uids = append(uids, variant.ModelUID)
		}
	}
	if len(uids) == 0 {
		t.Fatal("Devin snapshot has no model UIDs")
	}
	catalogBody, err := os.ReadFile("../../pkg/harness-compilation/modelpolicy/catalogs/devin.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var c catalog
	if err := yaml.Unmarshal(catalogBody, &c); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, records := range []map[string]catalogRecord{c.Models, c.Families} {
		for _, record := range records {
			if record.Model != "" {
				known[record.Model] = true
			}
			for _, entry := range record.Reasoning {
				if entry.Model != "" {
					known[entry.Model] = true
				}
			}
		}
	}
	var uncovered []string
	for _, uid := range uids {
		if known[uid] || strings.HasPrefix(uid, "fusion-") {
			continue
		}
		uncovered = append(uncovered, uid)
	}
	slices.Sort(uncovered)
	for _, uid := range uncovered {
		t.Errorf("uncovered Devin model_uid %q", uid)
	}
}

// spec: model-catalog-audit/summary-accepts-new-finding-codes
func TestSchemaOneSummaryAndProposeAcceptNewFindingCodes(t *testing.T) {
	r := report{Schema: 1, CheckedAt: "2026-11-05T10:00:00Z", Failed: true, Sources: []sourceReport{
		{Name: "gemini-api-models", Status: "unknown", Findings: []finding{{Code: "source.inventory-unparsed", Message: "No model identifiers found."}}},
		{Name: "factory-models", Harness: "factory", Status: "drift", Findings: []finding{{Code: "catalog.family-pin-stale", Model: "anthropic/haiku", Message: "A newer Haiku was observed."}}},
	}}
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var summary strings.Builder
	if err := run([]string{"summary"}, bytes.NewReader(body), &summary, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"source.inventory-unparsed", "catalog.family-pin-stale"} {
		if !strings.Contains(summary.String(), code) {
			t.Fatalf("summary omitted %s: %s", code, summary.String())
		}
	}
	root := writeProposeTree(t, map[string]string{
		"claude":  "schema: 1\nharness: claude\nsurfaces: {subagent: {model_field: model}}\n",
		"factory": "schema: 1\nharness: factory\nsurfaces: {subagent: {model_field: model}}\n",
		"gemini":  "schema: 1\nharness: gemini\nsurfaces: {subagent: {model_field: model}}\n",
		"devin":   "schema: 1\nharness: devin\nsurfaces: {subagent: {model_field: model}}\n",
	})
	var proposal strings.Builder
	if err := run([]string{"propose", "--root", root}, bytes.NewReader(body), &proposal, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
}
