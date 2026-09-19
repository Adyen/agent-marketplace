package ingest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestSkippableResourceDiagnostic(t *testing.T) {
	for _, tc := range []struct {
		code     string
		resource string
		want     bool
	}{
		{diag.CodeIngestFactoryLoadSkillFailed, "skills/bad", true},
		{diag.CodeIngestFactoryLoadCommandFailed, "commands/bad.md", true},
		{diag.CodeIngestFactoryLoadSkillFailed, "", false},
		{"security.path-escape", "skills/bad", false},
		{"ingest.unknown.load-skill-failed", "skills/bad", false},
	} {
		d := diag.Diagnostic{Code: tc.code, Resource: tc.resource, Severity: diag.SeverityError}
		if got := skippableResourceDiagnostic(d); got != tc.want {
			t.Fatalf("%+v: got %v", tc, got)
		}
	}
}

func BenchmarkSkippableResourceDiagnostic(b *testing.B) {
	d := diag.Diagnostic{Code: diag.CodeIngestFactoryLoadSkillFailed, Resource: "skills/bad", Severity: diag.SeverityError}
	for b.Loop() {
		skippableResourceDiagnostic(d)
	}
}

func TestMalformedSkillsDoNotDiscardValidResources(t *testing.T) {
	for _, harness := range []string{"factory", "claude"} {
		for _, healthy := range []bool{false, true} {
			t.Run(harness+"/healthy="+map[bool]string{true: "yes", false: "no"}[healthy], func(t *testing.T) {
				repo := t.TempDir()
				write := func(path, body string) {
					t.Helper()
					path = filepath.Join(repo, path)
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				write("broken/skills/bad/SKILL.md", "---\nname: bad\ndescription: invalid: yaml\n---\nBad\n")
				write("broken/commands/bad.md", "---\ndescription: invalid: yaml\n---\nBad\n")
				roots := []store.RegistryRoot{{Path: "broken", SourceHarness: harness}}
				if healthy {
					write("healthy/skills/good/SKILL.md", "---\nname: good\ndescription: Valid skill\nlicense: MIT\n---\nGood\n")
					roots = append(roots, store.RegistryRoot{Path: "healthy", SourceHarness: harness})
				}
				registry := store.Registry{ID: "partial", Slug: "partial", Origins: []string{"https://example.test/repo.git"}, Ref: "main", Roots: roots, Acquisition: store.AcquisitionMirror, License: "MIT"}
				rev, err := BuildRegistryRevision(context.Background(), testConfig(t, repo), "mp-test", registry, repo, strings.Repeat("a", 40))
				if !healthy {
					if !errors.Is(err, ErrNoValidResources) {
						t.Fatalf("all malformed: expected empty-publication guard, got %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(rev.Resources) != 1 || rev.Resources[0].Coordinate.Name != "good" {
					t.Fatalf("valid resources: %+v", rev.Resources)
				}
				found := false
				for _, d := range rev.Diagnostics {
					if strings.HasSuffix(d.Code, ".load-skill-failed") {
						found = true
						if d.Error || d.Root != "broken" || !strings.Contains(d.Message, "bad") {
							t.Fatalf("warning: %+v", d)
						}
					}
				}
				if !found {
					t.Fatal("missing malformed skill warning")
				}
			})
		}
	}
}
