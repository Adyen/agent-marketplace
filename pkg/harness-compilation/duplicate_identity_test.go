package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// spec: harness-compilation/windsurf-workflows-with-the-same-name-are-refused
func TestWindsurfWorkflowsWithSameNameAreRefused(t *testing.T) {
	src := fstest.MapFS{
		".windsurf/workflows/deploy.md": {Data: []byte("---\nname: deploy\ndescription: Approved deployment\n---\nINERT APPROVED BODY\n")},
		".windsurf/workflows/zzz.md":    {Data: []byte("---\nname: deploy\ndescription: Replacement deployment\n---\nINERT REPLACEMENT BODY\n")},
		".windsurf/workflows/format.md": {Data: []byte("---\ndescription: Format code\n---\nINERT FORMAT BODY\n")},
	}
	for _, target := range []Target{TargetWindsurf, TargetFactory} {
		t.Run(string(target), func(t *testing.T) {
			result, err := Convert(src, HarnessWindsurf, target)
			if err != nil {
				t.Fatal(err)
			}
			var refused, format bool
			for _, d := range result.Diagnostics {
				refused = refused || d.Code == diag.CodeIngestWindsurfDuplicateIdentity && d.Severity == diag.SeverityError
			}
			for _, a := range result.Artifacts {
				body := string(a.Bytes)
				if strings.Contains(body, "INERT APPROVED BODY") || strings.Contains(body, "INERT REPLACEMENT BODY") {
					t.Fatalf("ambiguous workflow emitted at %s:\n%s", a.Path, body)
				}
				format = format || strings.Contains(body, "INERT FORMAT BODY")
			}
			if !refused || !format {
				t.Fatalf("refused=%t format=%t diagnostics=%+v", refused, format, result.Diagnostics)
			}
		})
	}
}

// spec: harness-compilation/rule-frontmatter-with-a-repeated-key-is-refused
func TestRuleFrontmatterWithRepeatedKeyIsRefused(t *testing.T) {
	src := fstest.MapFS{".cursor/rules/globalize.mdc": {Data: []byte("---\ndescription: Apparently scoped rule\nglobs: src/safe/**\nalwaysApply: false\nalwaysApply: true\n---\nInert rule body.\n")}}
	for _, target := range []Target{TargetCursor, TargetWindsurf} {
		t.Run(string(target), func(t *testing.T) {
			result, err := Convert(src, HarnessCursor, target)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range result.Artifacts {
				if strings.Contains(string(a.Bytes), "Inert rule body.") {
					t.Fatalf("rule with a repeated key emitted at %s:\n%s", a.Path, a.Bytes)
				}
			}
			if !result.HasErrors() {
				t.Fatalf("repeated key accepted without an error: %+v", result.Diagnostics)
			}
		})
	}
}
