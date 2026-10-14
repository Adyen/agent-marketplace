package main

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const testCatalog = `schema: 1
harness: factory
evidence: {checked: "2026-09-21"}
surfaces: {subagent: {model_field: model}}
models:
  anthropic/old:
    model: old
  anthropic/current:
    model: current
    aliases: [current-alias]
    reasoning:
      high: {effort: high}
families:
  anthropic/opus: {pin: anthropic/old}
`

func TestAuditReportsInventoryDriftWithoutWriting(t *testing.T) {
	files := fstest.MapFS{catalogPath("factory"): &fstest.MapFile{Data: []byte(testCatalog)}}
	before := string(files[catalogPath("factory")].Data)
	s := source{Name: "factory-models", Harness: "factory", URL: "https://docs.factory.ai/docs/models.md", Kind: "factory"}
	body := "| Model | Model ID | Multiplier | Reasoning |\n| --- | --- | --- | --- |\n| Current | `current` | 1 | `high` |\n| New | `new` | 1 | `low` |"
	result := audit(context.Background(), files, []source{s}, nil, func(context.Context, string) (fetched, error) {
		return fetched{Body: []byte(body), URL: s.URL}, nil
	}, time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC))
	if !result.Failed || len(result.Sources) != 1 || result.Sources[0].Status != "drift" {
		t.Fatalf("expected drift: %#v", result)
	}
	var codes []string
	for _, f := range result.Sources[0].Findings {
		codes = append(codes, f.Code+":"+f.Model)
	}
	want := []string{"catalog.model-missing:new", "catalog.model-not-observed:old", "catalog.family-pin-review:anthropic/opus"}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("findings = %v, want %v", codes, want)
	}
	if string(files[catalogPath("factory")].Data) != before || len(files) != 1 {
		t.Fatal("audit mutated input files")
	}
	if result.Sources[0].BodySHA256 == "" || result.Sources[0].CatalogSHA256 == "" || result.CheckedAt != "2026-09-23T00:00:00Z" {
		t.Fatal("missing audit provenance")
	}
}

func TestAuditContinuesAfterUnavailableAndMalformedSources(t *testing.T) {
	files := fstest.MapFS{catalogPath("factory"): &fstest.MapFile{Data: []byte(testCatalog)}}
	sources := []source{
		{Name: "unavailable", Harness: "factory", URL: "unavailable", Kind: "factory"},
		{Name: "malformed", Harness: "factory", URL: "malformed", Kind: "factory"},
		{Name: "valid", Harness: "factory", URL: "valid", Kind: "factory"},
	}
	result := audit(context.Background(), files, sources, nil, func(_ context.Context, url string) (fetched, error) {
		if url == "unavailable" {
			return fetched{}, errors.New("transport failed with private proxy detail")
		}
		body := "<html>Sign in</html>"
		if url == "valid" {
			body = "| Model | Model ID | Multiplier | Reasoning |\n| --- | --- | --- | --- |\n| Current | `current` | 1 | `high` |\n| Old | `old` | 1 | `low` |"
		}
		return fetched{Body: []byte(body), URL: url}, nil
	}, time.Now())
	if !result.Failed || len(result.Sources) != 3 || result.Sources[0].Status != "unknown" || result.Sources[1].Status != "unknown" || result.Sources[2].Status != "no-drift" {
		t.Fatalf("partial audit = %#v", result)
	}
	if result.Sources[0].Findings[0].Code != "source.fetch-failed" || result.Sources[1].Findings[0].Code != "source.parse-failed" {
		t.Fatal("source failures must be distinguishable from drift")
	}
	if strings.Contains(result.Sources[0].Findings[0].Message, "private") {
		t.Fatal("transport details leaked")
	}
}

func TestDocumentWatchNeedsBaselineAndDetectsChanges(t *testing.T) {
	files := fstest.MapFS{catalogPath("factory"): &fstest.MapFile{Data: []byte(testCatalog)}}
	s := source{Name: "contract", Harness: "factory", Kind: "watch", Fields: []string{"model"}}
	body := "| `model` | inherit or model ID |"
	for _, tt := range []struct {
		name     string
		baseline map[string]baseline
		want     string
	}{
		{"missing", nil, "unknown"},
		{"unchanged", map[string]baseline{"contract": {Checked: "2026-09-23", SHA256: digest([]byte(body))}}, "no-drift"},
		{"changed", map[string]baseline{"contract": {Checked: "2026-09-23", SHA256: digest([]byte("old contract"))}}, "drift"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := audit(context.Background(), files, []source{s}, tt.baseline, func(context.Context, string) (fetched, error) { return fetched{Body: []byte(body)}, nil }, time.Now())
			if result.Sources[0].Status != tt.want || result.Failed != (tt.want != "no-drift") {
				t.Fatalf("result = %#v", result)
			}
			if result.Sources[0].Observed.Text != body {
				t.Fatal("report must retain inspectable selected evidence")
			}
		})
	}
}

func TestAuditCatalogFailureCannotPass(t *testing.T) {
	for _, files := range []fs.FS{fstest.MapFS{}, fstest.MapFS{catalogPath("factory"): &fstest.MapFile{Data: []byte("harness: wrong")}}} {
		result := audit(context.Background(), files, []source{{Name: "models", Harness: "factory", Kind: "factory"}}, nil, func(context.Context, string) (fetched, error) {
			t.Fatal("should validate catalog before fetching")
			return fetched{}, nil
		}, time.Now())
		if !result.Failed || result.Sources[0].Findings[0].Code != "catalog.invalid" {
			t.Fatalf("invalid catalog passed: %#v", result)
		}
	}
}
