package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	factoryingest "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/factory"
)

func factoryIdentityFixture(root, suffix string) fstest.MapFS {
	return fstest.MapFS{
		root + "/source" + suffix:               {Data: []byte("---\nname: review\ndescription: First\n---\nFIRST RESOURCE\n")},
		".factory/" + root + "/alias" + suffix:  {Data: []byte("---\nname: review\ndescription: Shadow\n---\nSHADOW RESOURCE\n")},
		".factory/" + root + "/review" + suffix: {Data: []byte("---\nname: other\ndescription: Unique\n---\nUNIQUE RESOURCE\n")},
	}
}

func TestFactoryIdentityUsesDeclaredNames(t *testing.T) {
	for _, tc := range []struct{ root, suffix, output string }{{"commands", ".md", "commands/review.md"}, {"droids", ".md", "droids/review.md"}, {"skills", "/SKILL.md", "skills/review/SKILL.md"}, {"rules", ".mdc", "rules/review.md"}} {
		t.Run(tc.root, func(t *testing.T) {
			src := factoryIdentityFixture(tc.root, tc.suffix)
			bundle, err := factoryingest.New().LoadBundle(src)
			if err != nil {
				t.Fatal(err)
			}
			names := map[string]int{}
			for _, r := range bundle.Resources {
				names[r.Name()]++
			}
			if len(bundle.Resources) != 2 || names["review"] != 1 || names["other"] != 1 {
				t.Errorf("declared identity mismatch: %v", names)
			}
			if !diag.HasCode(bundle.Diagnostics, "ingest.factory.resource-shadowed") {
				t.Errorf("shadowing was not diagnosed: %v", bundle.Diagnostics)
			}
			result, err := Convert(src, HarnessFactory, TargetFactory)
			if err != nil || result.HasErrors() {
				t.Fatalf("collision suppressed valid winner: %v %+v", err, result)
			}
			found := false
			for _, a := range result.Artifacts {
				if a.Path == tc.output {
					found = true
					if !strings.Contains(string(a.Bytes), "FIRST RESOURCE") || strings.Contains(string(a.Bytes), "SHADOW RESOURCE") {
						t.Fatalf("precedence changed: %s", a.Bytes)
					}
				}
			}
			if !found {
				t.Fatal("winner artifact missing")
			}
		})
	}
}

func TestFactoryIdentityRemainsKindScoped(t *testing.T) {
	src := fstest.MapFS{
		"commands/review.md":     {Data: []byte("---\nname: review\ndescription: Command\n---\nCommand body\n")},
		"droids/review.md":       {Data: []byte("---\nname: review\ndescription: Droid\n---\nDroid body\n")},
		"skills/review/SKILL.md": {Data: []byte("---\nname: review\ndescription: Skill\n---\nSkill body\n")},
	}
	bundle, err := factoryingest.New().LoadBundle(src)
	if err != nil || len(bundle.Resources) != 3 || diag.HasCode(bundle.Diagnostics, "ingest.factory.resource-shadowed") {
		t.Fatalf("different kinds were collapsed: %v %+v", err, bundle)
	}
}

func TestFactoryShadowedDroidOmitsInternalDiagnostics(t *testing.T) {
	src := fstest.MapFS{
		"droids/review.md":         {Data: []byte("---\nname: review\ndescription: Winner\n---\nWinner\n")},
		".factory/droids/alias.md": {Data: []byte("---\nname: review\ndescription: Shadow\nmodel: claude-sonnet-4-5-20250929\nreasoningEffort: high\n---\nShadow\n")},
	}
	bundle, err := factoryingest.New().LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 || !diag.HasCode(bundle.Diagnostics, diag.CodeIngestFactoryResourceShadowed) {
		t.Fatalf("wrong shadow outcome: %+v", bundle)
	}
	if diag.HasCode(bundle.Diagnostics, diag.CodeIngestFactoryReasoningEffortMappedToModel) {
		t.Fatalf("ignored droid reported applied model mapping: %+v", bundle.Diagnostics)
	}
}
