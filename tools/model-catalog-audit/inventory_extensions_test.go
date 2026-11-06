package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	if !trustedURL("https://aistudio.google.com/docs/models") {
		t.Fatal("Gemini API documentation redirect destination is not trusted")
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
			}, time.Now(), nil)
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
	}, time.Now(), nil)
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
			}, time.Now(), nil)
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
			}, time.Now(), nil)
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

const cursorDocsFixture = `| Model | Provider | Input | Notes |
| --- | --- | --- | --- |
| Composer 2 | Cursor | $0 | Available |`

// spec: model-catalog-audit/cursor-cli-output-lists-model-identifiers
func TestCursorCLIOutputSuppliesCursorInventory(t *testing.T) {
	body, err := os.ReadFile("testdata/cursor-cli-models.txt")
	if err != nil {
		t.Fatal(err)
	}
	body = bytes.ReplaceAll(body, []byte("composer-2"), []byte("\x1b[36mcomposer-2\x1b[0m"))
	body = bytes.ReplaceAll(body, []byte("gpt-5.5"), []byte("\x1b[32mgpt-5.5\x1b[0m"))
	body = bytes.ReplaceAll(body, []byte("claude-4.5-sonnet-thinking"), []byte("\x1b[33mclaude-4.5-sonnet-thinking\x1b[0m"))
	path := filepath.Join(t.TempDir(), "cursor-cli-models.txt")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	got := runCursorAudit(t, []string{"-cursor-models", path})
	wantIDs := []string{"composer-2", "gpt-5.5", "claude-4.5-sonnet-thinking"}
	if ids := modelIDs(got.Observed.Models); !slices.Equal(ids, wantIDs) {
		t.Fatalf("cursor inventory = %v, want %v", ids, wantIDs)
	}
	var missing []string
	for _, finding := range got.Findings {
		if finding.Code == "catalog.model-missing" {
			missing = append(missing, finding.Model)
		}
	}
	if !slices.Equal(missing, wantIDs[1:]) {
		t.Fatalf("missing Cursor catalog IDs = %v, want %v", missing, wantIDs[1:])
	}
	if got.Observed.Text != "Composer 2 | Cursor | Available" {
		t.Fatalf("Cursor docs evidence changed: %q", got.Observed.Text)
	}
	if got.ObservedSHA256 != "e84b46b4791ce5f8a277dc11f971b754ed38baeb66de383270eae5c5071f60e7" {
		t.Fatalf("Cursor evidence hash = %s, want docs evidence hash", got.ObservedSHA256)
	}
}

// spec: model-catalog-audit/cursor-cli-output-is-missing-or-unauthenticated
func TestCursorCLIOutputMissingOrUnparseableIsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		path bool
	}{
		{name: "no file supplied"},
		{name: "empty file", path: true},
		{name: "unreadable file", path: true},
		{name: "authentication error", body: "Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable.", path: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var args []string
			if tc.path {
				path := filepath.Join(t.TempDir(), "cursor-cli-models.txt")
				if tc.name != "unreadable file" {
					if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				args = []string{"-cursor-models", path}
			}
			got := runCursorAudit(t, args)
			if got.Status != "unknown" || !slices.ContainsFunc(got.Findings, func(f finding) bool {
				return f.Code == "source.inventory-unparsed"
			}) {
				t.Fatalf("Cursor source for %s = %#v, want source.inventory-unparsed", tc.name, got)
			}
			if len(got.Observed.Models) != 0 {
				t.Fatalf("parsed prose from %s as models: %#v", tc.name, got.Observed.Models)
			}
		})
	}
}

func TestReadCursorCLIModelsLogsEmptyInventoryReasons(t *testing.T) {
	dir := t.TempDir()
	noModelsPath := filepath.Join(dir, "no-models.txt")
	if err := os.WriteFile(noModelsPath, []byte("not a model listing"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path, reason string
	}{
		{name: "flag not set", reason: "flag not set"},
		{name: "open error", path: filepath.Join(dir, "missing.txt"), reason: "open error"},
		{name: "read error", path: dir, reason: "read error"},
		{name: "no model lines", path: noModelsPath, reason: "no <id> - <display name> lines"},
	}

	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	defer slog.SetDefault(previous)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output.Reset()
			if models := readCursorCLIModels(tt.path); len(models) != 0 {
				t.Fatalf("models = %#v, want an empty inventory", models)
			}
			log := output.String()
			if !strings.Contains(log, "level=WARN") || !strings.Contains(log, tt.reason) ||
				!strings.Contains(log, "path=") || !strings.Contains(log, "error=") {
				t.Fatalf("warning = %q, want reason, path, and error", log)
			}
			if tt.path != "" && !strings.Contains(log, tt.path) {
				t.Fatalf("warning = %q, want path %q", log, tt.path)
			}
		})
	}
}

func runCursorAudit(t *testing.T, cursorArgs []string) sourceReport {
	t.Helper()
	var out bytes.Buffer
	args := append([]string{"check", "--root", "../.."}, cursorArgs...)
	err := run(args, strings.NewReader(""), &out, func(_ context.Context, endpoint string) (fetched, error) {
		if endpoint == "https://cursor.com/docs/models-and-pricing.md" {
			return fetched{Body: []byte(cursorDocsFixture), URL: endpoint}, nil
		}
		return fetched{}, errors.New("offline test")
	}, time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, errAuditFailed) {
		t.Fatalf("check error = %v, want audit findings", err)
	}
	var result report
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode check report %q: %v", out.String(), err)
	}
	for _, source := range result.Sources {
		if source.Name == "cursor-models" {
			return source
		}
	}
	t.Fatalf("check report omitted cursor-models: %s", out.String())
	return sourceReport{}
}

func modelIDs(models []observedModel) []string {
	ids := make([]string, len(models))
	for i, model := range models {
		ids[i] = model.ID
	}
	return ids
}
