package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSummaryReportsFixtureOnlyTrust(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	for _, scope := range []string{"repo", "user"} {
		body := `{"target":"gemini","native_mode":"probe","results":[{"harness":"gemini","resource":"skill","scope":"` + scope + `","materialization_mode":"file","status":"native-verified","observed_version":"1.2.3","reason":"discovered fixture","fixture_trust":"repository-only"}]}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		now := time.Now()
		err := summarize(&out, path, "fixture-image", now.Add(-time.Hour).Format(time.RFC3339), "bootstrap", now)
		if err != nil || !strings.Contains(out.String(), "untrusted gate verified before repository-only fixture trust was granted") {
			t.Fatalf("execution-workspace trust must remain visible independently of resource scope: %v\n%s", err, out.String())
		}
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(body, "repository-only", "all-repositories")), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := summarize(&strings.Builder{}, path, "fixture-image", now.Format(time.RFC3339), "bootstrap", now); err == nil {
			t.Fatal("broad trust was accepted as fixture-only trust")
		}
	}
}

func TestSummaryFailedResourcesAndFreshBootstrap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, status := range []string{"failed", "blocked", "config-only-verified"} {
		body := `{"target":"claude","native_mode":"probe","results":[{"harness":"claude","resource":"mcp","scope":"repo","materialization_mode":"file","status":"` + status + `","reason":"independent fixture result"}]}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		err := summarize(&out, path, "bootstrap-image", now.Add(-47*time.Hour-59*time.Minute).Format(time.RFC3339), "bootstrap", now)
		if (err != nil) != (status != "config-only-verified") {
			t.Errorf("wrong failure decision for %s: %v", status, err)
		}
		for _, want := range []string{"bootstrap", "47h", status, "independent fixture result"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("summary missing %s", want)
			}
		}
		if strings.Contains(out.String(), "STALE") {
			t.Fatal("fresh image was labelled stale")
		}
	}
}

func TestSummaryPreservesEvidenceAndEscapesMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	body := `{"target":"devin","native_mode":"probe","expected_version":"1.2.3","results":[
{"harness":"devin","resource":"native-cli","scope":"repo","materialization_mode":"file","status":"native-verified","observed_version":"1.2.3","reason":"version and negative controls only"},
{"harness":"devin","resource":"hook","scope":"repo","materialization_mode":"file","status":"config-only-verified","observed_version":"1.2.3","reason":"No session | <script>alert(1)</script>\n[bad](javascript:evil)"},
{"harness":"devin","resource":"mcp","scope":"repo","materialization_mode":"file","status":"native-verified","observed_version":"1.2.3","reason":"names and command recognized","connection":{"status":"blocked","reason":"pending project trust"}},
{"harness":"devin","resource":"rule","scope":"repo","materialization_mode":"file","status":"unsupported-as-expected","reason":"rule.devin.unsupported"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	created := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	err := summarize(&out, path, "ghcr.io/owner/repo/image@sha256:"+strings.Repeat("a", 64), created.Format(time.RFC3339), "published", created.Add(49*time.Hour))
	if err == nil {
		t.Fatal("blocked connection was reported as a green summary")
	}
	text := out.String()
	for _, want := range []string{"1.2.3", "49h", "STALE", "published", "CLI setup only", "Resource evidence", "config-only-verified", "unsupported-as-expected", "pending project trust", "names and command recognized", `\|`, "&lt;script&gt;", `\[bad\]`} {
		if !strings.Contains(text, want) {
			t.Errorf("summary missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"<script>", "\n[bad]", "[bad](javascript:", "No session |"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("unsafe Markdown: %q", forbidden)
		}
	}
}

func TestSummaryMissingMalformedAndConfigOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	for _, body := range []string{"", "{", `{}`, `{"target":"devin","results":[]}`, `{"target":"devin","results":[{"status":"green"}]}`, `{"target":"devin","results":[{}]}`} {
		if body != "" {
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		var out strings.Builder
		if err := summarize(&out, path, "", "", "config-only", time.Now()); err == nil || !strings.Contains(out.String(), "Report unavailable or invalid") {
			t.Errorf("missing/malformed report was not explicit: %v\n%s", err, out.String())
		}
	}
	body := `{"target":"windsurf","native_mode":"unavailable","results":[{"harness":"windsurf","resource":"skill","scope":"user","materialization_mode":"symlink","status":"config-only-verified","reason":"no verified credential-free CLI"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := run([]string{"summary", "--report", path, "--source", "config-only"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"config-only", "not observed", "no verified credential-free CLI"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary lost gap %q", want)
		}
	}
	for _, tc := range []struct{ created, source string }{{"not-a-time", "published"}, {"", "published"}, {"", "invented"}} {
		if err := summarize(&strings.Builder{}, path, "image", tc.created, tc.source, time.Now()); err == nil {
			t.Errorf("invalid provenance accepted: %+v", tc)
		}
	}
}
