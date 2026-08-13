package ingest_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
)

func TestLoadFailureUsesPerHarnessCodeForPlainErrors(t *testing.T) {
	d := ingest.LoadFailure(diag.CodeIngestClaudeLoadSkillFailed, "skill", "my-skill", "skills/my-skill", errors.New("boom"))

	if d.Code != diag.CodeIngestClaudeLoadSkillFailed {
		t.Fatalf("code = %q, want %q", d.Code, diag.CodeIngestClaudeLoadSkillFailed)
	}
	if d.Severity != diag.SeverityError {
		t.Fatalf("severity = %v, want error", d.Severity)
	}
	if d.Resource != "my-skill" {
		t.Fatalf("resource = %q, want %q", d.Resource, "my-skill")
	}
	if d.Source.File != "skills/my-skill" || d.Source.Line != 1 {
		t.Fatalf("source = %+v, want {skills/my-skill 1}", d.Source)
	}
	if !strings.Contains(d.Message, `failed to load skill "skills/my-skill": boom`) {
		t.Fatalf("message = %q", d.Message)
	}
}

// A coded JSON failure must win over the per-harness load-failed code so
// duplicate keys are keyed identically no matter which source harness produced
// the failure.
func TestLoadFailurePrefersCodedJSONFailure(t *testing.T) {
	coded := diag.NewCodedError(diag.CodeJSONDuplicateKey, diag.SourceRef{File: ".mcp.json", Line: 7}, errors.New("duplicate JSON object key \"command\" at $.mcpServers.docs"))

	d := ingest.LoadFailure(diag.CodeIngestCursorLoadSkillFailed, "skill", "my-skill", "skills/my-skill", coded)

	if d.Code != diag.CodeJSONDuplicateKey {
		t.Fatalf("code = %q, want %q", d.Code, diag.CodeJSONDuplicateKey)
	}
	if d.Source.File != ".mcp.json" || d.Source.Line != 7 {
		t.Fatalf("source = %+v, want {.mcp.json 7}", d.Source)
	}
	if d.Hint == "" {
		t.Fatal("hint is empty, want the catalog hint for the coded failure")
	}
	if d.Severity != diag.SeverityError {
		t.Fatalf("severity = %v, want error", d.Severity)
	}
}

func TestLoadJSONManifestRejectsDuplicateKeys(t *testing.T) {
	src := fstest.MapFS{"plugin.json": {Data: []byte(`{"name":"a","name":"b"}`)}}
	var manifest struct {
		Name string `json:"name"`
	}
	err := ingest.LoadJSONManifest(src, "plugin.json", &manifest)
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("error %T does not carry a stable diagnostic code", err)
	}
	if coded.Code != diag.CodeJSONDuplicateKey {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeJSONDuplicateKey)
	}
	if manifest.Name != "" {
		t.Fatalf("manifest.Name = %q, want the destination left untouched", manifest.Name)
	}
}

// A missing manifest must stay recognizable so ingesters can synthesize one.
func TestLoadJSONManifestReportsMissingFileUnwrapped(t *testing.T) {
	var manifest struct{}
	err := ingest.LoadJSONManifest(fstest.MapFS{}, "plugin.json", &manifest)
	if err == nil {
		t.Fatal("LoadJSONManifest accepted a missing file")
	}
	if _, coded := diag.AsCodedError(err); coded {
		t.Fatalf("missing file was wrapped in a CodedError: %v", err)
	}
}
