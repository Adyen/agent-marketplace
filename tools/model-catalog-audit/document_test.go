package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"gopkg.in/yaml.v3"
)

const testHookCatalog = `schema: 1
harness: factory
evidence:
  checked: "2026-10-07"
  refs:
    - https://docs.factory.ai/reference/hooks-reference
events:
  PreToolUse: {}
`

const testToolCatalog = `schema: 1
harness: windsurf
evidence:
  checked: "2026-10-07"
  refs:
    - https://docs.devin.ai/desktop/cascade/hooks
tools: {}
surfaces: []
`

func documentCatalogFS() fstest.MapFS {
	return fstest.MapFS{
		"pkg/harness-compilation/hookpolicy/catalogs/factory.yaml":  &fstest.MapFile{Data: []byte(testHookCatalog)},
		"pkg/harness-compilation/toolpolicy/catalogs/windsurf.yaml": &fstest.MapFile{Data: []byte(testToolCatalog)},
	}
}

var documentSource = source{
	Name: "hook-factory", Harness: "factory", Domain: "hookpolicy",
	URL:      "https://docs.factory.ai/reference/hooks-reference",
	Kind:     "document",
	Sections: []string{"Hook input", "Common matchers"},
}

var documentSourceToolControl = source{
	Name: "tool-windsurf", Harness: "windsurf", Domain: "toolpolicy",
	URL:      "https://docs.devin.ai/desktop/cascade/hooks",
	Kind:     "document",
	Sections: []string{"Hook Events"},
}

const documentBody = "# Hook input\n\nHooks  accept   matchers.\n## Common matchers\n\nList.\n"

const documentNormalized = "# Hook input Hooks accept matchers. ## Common matchers List."

func TestDocumentSourceReportsStatus(t *testing.T) {
	for _, tt := range []struct {
		name      string
		source    source
		body      string
		baselines map[string]baseline
		fetchErr  error
		want      string
		wantCode  string
	}{
		{name: "unchanged", source: documentSource, body: documentBody,
			baselines: map[string]baseline{"hook-factory": {Checked: "2026-10-07", SHA256: digest([]byte(documentNormalized))}},
			want:      "no-drift"},
		{name: "changed", source: documentSource, body: documentBody + "new\n",
			baselines: map[string]baseline{"hook-factory": {Checked: "2026-10-07", SHA256: digest([]byte(documentNormalized))}},
			want:      "drift", wantCode: "source.evidence-changed"},
		{name: "baseline-missing", source: documentSource, body: documentBody,
			baselines: nil,
			want:      "unknown", wantCode: "source.baseline-missing"},
		{name: "fetch-failed", source: documentSource, fetchErr: errors.New("transport refused"),
			baselines: map[string]baseline{"hook-factory": {Checked: "2026-10-07", SHA256: digest([]byte(documentNormalized))}},
			want:      "unknown", wantCode: "source.fetch-failed"},
		{name: "missing-marker", source: documentSource, body: "# Unrelated\n\nnothing\n",
			baselines: map[string]baseline{"hook-factory": {Checked: "2026-10-07", SHA256: digest([]byte(documentNormalized))}},
			want:      "unknown", wantCode: "source.parse-failed"},
		{name: "empty", source: documentSource, body: "   \n\n  ",
			baselines: map[string]baseline{"hook-factory": {Checked: "2026-10-07", SHA256: digest([]byte(documentNormalized))}},
			want:      "unknown", wantCode: "source.parse-failed"},
		{name: "ambiguous", source: documentSource, body: "<!doctype html><html><body><article>alpha</article><article>different beta</article></body></html>",
			baselines: map[string]baseline{"hook-factory": {Checked: "2026-10-07", SHA256: digest([]byte(documentNormalized))}},
			want:      "unknown", wantCode: "source.parse-failed"},
		{name: "windsurf-tool-control", source: documentSourceToolControl, body: "# Hook Events\n\nlist\n",
			baselines: map[string]baseline{"tool-windsurf": {Checked: "2026-10-07", SHA256: digest([]byte("# Hook Events list"))}},
			want:      "no-drift"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := audit(context.Background(), documentCatalogFS(), []source{tt.source}, tt.baselines, func(context.Context, string) (fetched, error) {
				if tt.fetchErr != nil {
					return fetched{}, tt.fetchErr
				}
				return fetched{Body: []byte(tt.body), URL: tt.source.URL}, nil
			}, time.Now())
			if len(result.Sources) != 1 {
				t.Fatalf("sources = %#v", result.Sources)
			}
			report := result.Sources[0]
			if report.Status != tt.want {
				t.Fatalf("status = %q, want %q", report.Status, tt.want)
			}
			if result.Failed != (tt.want != "no-drift") {
				t.Fatalf("failed = %t, want %t", result.Failed, tt.want != "no-drift")
			}
			if tt.wantCode != "" && (len(report.Findings) == 0 || report.Findings[0].Code != tt.wantCode) {
				t.Fatalf("findings = %#v, want %q", report.Findings, tt.wantCode)
			}
			if tt.wantCode == "" && len(report.Findings) != 0 {
				t.Fatalf("findings = %#v, want none", report.Findings)
			}
			if report.CatalogDomain != tt.source.Domain {
				t.Fatalf("catalog_domain = %q, want %q", report.CatalogDomain, tt.source.Domain)
			}
			if tt.wantCode != "source.fetch-failed" && tt.wantCode != "catalog.invalid" && report.CatalogSHA256 == "" {
				t.Fatal("document audit must record the catalog digest")
			}
		})
	}
}

func TestDocumentSourceRejectsMismatchedCatalogs(t *testing.T) {
	fetch := func(context.Context, string) (fetched, error) {
		t.Fatal("must not fetch before catalog validation")
		return fetched{}, nil
	}
	for _, tt := range []struct {
		name   string
		source source
		files  fstest.MapFS
	}{
		{name: "unsupported-domain", source: source{Name: "x", Harness: "factory", Domain: "modelpolicy", URL: "https://example.test", Kind: "document"}, files: documentCatalogFS()},
		{name: "wrong-harness", source: source{Name: "x", Harness: "cursor", Domain: "hookpolicy", URL: "https://docs.factory.ai/reference/hooks-reference", Kind: "document"}, files: documentCatalogFS()},
		{name: "ref-not-declared", source: source{Name: "x", Harness: "factory", Domain: "hookpolicy", URL: "https://example.test/elsewhere", Kind: "document"}, files: documentCatalogFS()},
		{name: "catalog-missing", source: source{Name: "x", Harness: "gemini", Domain: "hookpolicy", URL: "https://docs.factory.ai/reference/hooks-reference", Kind: "document"}, files: documentCatalogFS()},
		{name: "hook-catalog-no-events", source: source{Name: "x", Harness: "windsurf", Domain: "hookpolicy", URL: "https://docs.devin.ai/desktop/cascade/hooks", Kind: "document"},
			files: fstest.MapFS{"pkg/harness-compilation/hookpolicy/catalogs/windsurf.yaml": &fstest.MapFile{Data: []byte("schema: 1\nharness: windsurf\nevidence: {refs: [https://docs.devin.ai/desktop/cascade/hooks]}\nevents: {}\n")}}},
		{name: "tool-catalog-no-surfaces", source: source{Name: "x", Harness: "factory", Domain: "toolpolicy", URL: "https://docs.factory.ai/reference/hooks-reference", Kind: "document"},
			files: fstest.MapFS{"pkg/harness-compilation/toolpolicy/catalogs/factory.yaml": &fstest.MapFile{Data: []byte("schema: 1\nharness: factory\nevidence: {refs: [https://docs.factory.ai/reference/hooks-reference]}\ntools: {Bash: {}}\n")}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := audit(context.Background(), tt.files, []source{tt.source}, nil, fetch, time.Now())
			if len(result.Sources) != 1 || result.Sources[0].Findings[0].Code != "catalog.invalid" || !result.Failed {
				t.Fatalf("result = %#v, want catalog.invalid", result)
			}
		})
	}
}

func TestDocumentTextNormalization(t *testing.T) {
	for _, tt := range []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{name: "markdown", body: "# Title\n\n  Some   spaced   text\n\n## Next\n", want: "# Title Some spaced text ## Next"},
		{name: "html-article", body: `<!doctype html><html><body><nav>skip me</nav><article> Keep   this
			<script>var x=1</script>text </article><footer>skip too</footer></body></html>`, want: "Keep this text"},
		{name: "identical-duplicate-mains", body: `<html><body><main>same content</main><div><main>same content</main></div></body></html>`, want: "same content"},
		{name: "distinct-roots-refused", body: `<html><body><main>first</main><main>second</main></body></html>`, wantErr: true},
		{name: "no-article-or-main", body: `<html><body><div>nothing selected</div></body></html>`, wantErr: true},
		{name: "empty", body: " \n\t ", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			text, err := documentText([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("documentText(%q) = %q, want refusal", tt.body, text)
				}
				return
			}
			if err != nil || text != tt.want {
				t.Fatalf("documentText(%q) = %q, %v; want %q", tt.body, text, err, tt.want)
			}
		})
	}
}

// Offline integrity only: every catalog evidence ref is bound to a source
// the live audit monitors for that harness and domain, and every monitored
// source is cited. Model catalogs may also cite an immutable commit-pinned
// revision or a document in this repository; those cannot drift and are
// listed exactly. Passing says nothing about live freshness (the online
// audit) or about vendor-native model acceptance (harness probes).
func TestEveryCatalogEvidenceRefIsBound(t *testing.T) {
	monitored := map[string][]source{}
	modelSources := 0
	for _, s := range sources {
		monitored[s.Domain+"/"+s.Harness] = append(monitored[s.Domain+"/"+s.Harness], s)
		if s.Domain == "" {
			modelSources++
		}
	}
	if len(hookToolSources) != 33 || modelSources != 20 {
		t.Fatalf("hook/tool audit sources = %d, model audit sources = %d, want 33 and 20", len(hookToolSources), modelSources)
	}
	baselineBody, err := os.ReadFile("baselines.json")
	if err != nil {
		t.Fatal(err)
	}
	var baselines map[string]baseline
	if err := json.Unmarshal(baselineBody, &baselines); err != nil {
		t.Fatal(err)
	}
	pinnedRevision := regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/blob/[0-9a-f]{40}/[^?#]+$`)
	wantUnmonitored := []string{
		"modelpolicy/gemini pinned https://github.com/google-gemini/gemini-cli/blob/cfbcaa8df13ea4610bb379b377b56d62980c0032/packages/core/src/agents/agentLoader.ts",
		"modelpolicy/opencode repository docs/harness-validation/opencode.md",
		"modelpolicy/windsurf repository pkg/harness-compilation/field-matrix.md",
	}
	var unmonitored []string
	for _, domain := range []string{"modelpolicy", "hookpolicy", "toolpolicy"} {
		sourceDomain := domain
		if domain == "modelpolicy" {
			sourceDomain = ""
		}
		entries, err := os.ReadDir("../../pkg/harness-compilation/" + domain + "/catalogs")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 8 {
			t.Fatalf("%s catalogs = %d, want 8", domain, len(entries))
		}
		for _, entry := range entries {
			harness := strings.TrimSuffix(entry.Name(), ".yaml")
			body, err := os.ReadFile("../../pkg/harness-compilation/" + domain + "/catalogs/" + entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			var c struct {
				Evidence struct {
					Refs []string `yaml:"refs"`
				} `yaml:"evidence"`
			}
			if err := yaml.Unmarshal(body, &c); err != nil {
				t.Fatal(err)
			}
			if len(c.Evidence.Refs) == 0 {
				t.Fatalf("%s/%s cites no evidence", domain, harness)
			}
			seen := map[string]bool{}
			for _, ref := range c.Evidence.Refs {
				for _, s := range monitored[sourceDomain+"/"+harness] {
					seen[ref] = seen[ref] || s.URL == ref
				}
				switch {
				case seen[ref]:
				case domain == "modelpolicy" && pinnedRevision.MatchString(ref):
					unmonitored = append(unmonitored, domain+"/"+harness+" pinned "+ref)
				case domain == "modelpolicy" && !strings.Contains(ref, "://") && filepath.IsLocal(ref):
					info, err := os.Lstat(filepath.Join("../..", filepath.FromSlash(ref)))
					if err != nil || !info.Mode().IsRegular() {
						t.Fatalf("%s/%s repository evidence %q is not a regular file: %v", domain, harness, ref, err)
					}
					unmonitored = append(unmonitored, domain+"/"+harness+" repository "+ref)
				default:
					t.Fatalf("%s/%s evidence ref %q has no audit source", domain, harness, ref)
				}
			}
			for _, s := range monitored[sourceDomain+"/"+harness] {
				if !seen[s.URL] {
					t.Fatalf("audit source %s (%s) is not an evidence ref of %s/%s", s.Name, s.URL, domain, harness)
				}
				if _, ok := baselines[s.Name]; !ok {
					t.Fatalf("audit source %s has no reviewed baseline", s.Name)
				}
			}
		}
	}
	if !slices.Equal(unmonitored, wantUnmonitored) {
		t.Fatalf("unmonitored evidence = %q, want %q", unmonitored, wantUnmonitored)
	}
}

func TestTrustedURLAdmitsDocumentSourcesOnly(t *testing.T) {
	for _, raw := range []string{
		"https://docs.factory.com/reference/hooks-reference",
		"https://geminicli.com/docs/hooks/",
		"https://opencode.ai/docs/plugins.md",
		"https://github.com/google-gemini/gemini-cli/blob/main/docs/reference/tools.md",
		"https://github.com/google-gemini/gemini-cli/blob/main/docs/core/subagents.md",
		"https://github.com/google-gemini/gemini-cli/blob/main/docs/extensions/reference.md",
	} {
		if !trustedURL(raw) {
			t.Fatalf("trustedURL(%q) = false, want true", raw)
		}
	}
	for _, raw := range []string{
		"https://github.com/google-gemini/other-repo/blob/main/x.md",
		"https://github.com/unrelated/repo/blob/main/x.md",
		"http://geminicli.com/docs/hooks/",
		"https://user@docs.factory.com/x",
		"https://docs.factory.com/x?q=1",
		"https://geminicli.com/docs/hooks/#frag",
		"https://github.com/google-gemini/gemini-cli/../evil",
	} {
		if trustedURL(raw) {
			t.Fatalf("trustedURL(%q) = true, want false", raw)
		}
	}
}
