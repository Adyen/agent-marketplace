package diag

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSeverityString(t *testing.T) {
	tests := []struct {
		s    Severity
		want string
	}{
		{SeverityError, "error"},
		{SeverityWarning, "warning"},
		{SeverityInfo, "info"},
		{Severity(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", int(tt.s), got, tt.want)
		}
	}
}

func TestDiagnosticFields(t *testing.T) {
	d := Diagnostic{
		Code:     CodeSubagentWindsurfNoPrimitive,
		Severity: SeverityWarning,
		Target:   "windsurf",
		Resource: "reviewer",
		Message:  "Windsurf has no subagent primitive",
		Hint:     "Use a workflow instead",
		Source:   SourceRef{File: "agents/reviewer.md", Line: 1},
	}
	if d.Code != CodeSubagentWindsurfNoPrimitive {
		t.Errorf("Diagnostic.Code = %q, want %q", d.Code, CodeSubagentWindsurfNoPrimitive)
	}
	if d.Severity != SeverityWarning {
		t.Errorf("Diagnostic.Severity = %v, want %v", d.Severity, SeverityWarning)
	}
	if d.Source.File != "agents/reviewer.md" {
		t.Errorf("Diagnostic.Source.File = %q, want %q", d.Source.File, "agents/reviewer.md")
	}
}

func TestDocForCode(t *testing.T) {
	for _, code := range AllCodes() {
		doc, ok := DocForCode(code)
		if !ok {
			t.Errorf("DocForCode(%q): no entry found", code)
			continue
		}
		if doc.Code != code {
			t.Errorf("DocForCode(%q): returned doc with Code %q", code, doc.Code)
		}
		if doc.Summary == "" {
			t.Errorf("DocForCode(%q): empty Summary", code)
		}
	}

	// Unknown code
	_, ok := DocForCode("nonexistent.code")
	if ok {
		t.Error("DocForCode should return false for unknown codes")
	}
}

func TestCodexMaterializationCodesAreCatalogued(t *testing.T) {
	for _, code := range []string{
		CodeMaterializeCodexTrustReview,
		CodeMaterializeCodexTrustReviewPending,
		"materialize.codex.aggregate-malformed",
		"materialize.codex.aggregate-unpatchable",
		"materialize.destination.overlap",
	} {
		if _, ok := DocForCode(code); !ok {
			t.Errorf("DocForCode(%q): no entry found", code)
		}
	}
}

func TestDiagnosticCodes_Documented(t *testing.T) {
	// Parse codes.md to extract all documented codes.
	codeSet := parseCodesMD(t)

	for _, code := range AllCodes() {
		if !codeSet[code] {
			t.Errorf("code %q is not documented in codes.md", code)
		}
	}
}

func TestMatrixDiagnosticCodes_Documented(t *testing.T) {
	codeSet := parseCodesMD(t)
	for code := range collectMatrixDiagnosticCodes(t) {
		if !codeSet[code] {
			t.Errorf("matrix diagnostic code %q is not documented in codes.md", code)
		}
	}
}

func TestMatrixDiagnosticCodes_SubagentWindsurfDropCovered(t *testing.T) {
	codes := collectMatrixDiagnosticCodes(t)
	if !codes[CodeSubagentWindsurfNoPrimitive] {
		t.Fatalf("matrix fixtures do not exercise %q", CodeSubagentWindsurfNoPrimitive)
	}
}

func TestAllCodesCoversCatalog(t *testing.T) {
	// Verify AllCodes() covers every entry in codeCatalog.
	allSet := make(map[string]bool)
	for _, code := range AllCodes() {
		allSet[code] = true
	}
	for _, doc := range codeCatalog {
		if !allSet[doc.Code] {
			t.Errorf("codeCatalog entry %q not in AllCodes()", doc.Code)
		}
	}
}

func TestCodeSeveritiesMatchCatalog(t *testing.T) {
	// Verify each code constant's severity matches the catalog entry.
	for _, doc := range codeCatalog {
		// Every catalog entry must be findable by code
		found, ok := DocForCode(doc.Code)
		if !ok {
			t.Errorf("catalog entry %q not found by DocForCode", doc.Code)
			continue
		}
		if found.Severity != doc.Severity {
			t.Errorf("severity mismatch for %q: catalog=%v, DocForCode=%v", doc.Code, doc.Severity, found.Severity)
		}
	}
}

// parseCodesMD reads codes.md and extracts all backtick-enclosed code strings
// from the table rows.
func parseCodesMD(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open("codes.md")
	if err != nil {
		t.Fatalf("opening codes.md: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Logf("closing codes.md: %v", err)
		}
	}()

	codes := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Look for backtick-enclosed code strings in table rows like | `code` | ...
		for {
			start := strings.Index(line, "`")
			if start == -1 {
				break
			}
			end := strings.Index(line[start+1:], "`")
			if end == -1 {
				break
			}
			code := line[start+1 : start+1+end]
			if strings.Contains(code, ".") && !strings.Contains(code, " ") {
				// Looks like a dotted code identifier
				codes[code] = true
			}
			line = line[start+1+end+1:]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning codes.md: %v", err)
	}
	return codes
}

type matrixSpec struct {
	Cells []struct {
		Diagnostics []string `yaml:"diagnostics"`
	} `yaml:"cells"`
}

func collectMatrixDiagnosticCodes(t *testing.T) map[string]bool {
	t.Helper()

	root := filepath.Join("..", "testdata", "matrix")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}

	codes := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "matrix.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var spec matrixSpec
		if err := yaml.Unmarshal(data, &spec); err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, cell := range spec.Cells {
			for _, code := range cell.Diagnostics {
				codes[code] = true
			}
		}
	}
	return codes
}
