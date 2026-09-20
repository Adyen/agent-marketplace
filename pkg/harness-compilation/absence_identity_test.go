package harnesscompile

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func absenceIdentityFixture(source Harness) (fstest.MapFS, string) {
	skillPath, mcpPath := "skills/control/SKILL.md", "mcp.json"
	switch source {
	case HarnessClaude:
		mcpPath = ".mcp.json"
	case HarnessGemini:
		skillPath, mcpPath = ".gemini/skills/control/SKILL.md", ".gemini/mcp.json"
	case HarnessWindsurf:
		skillPath, mcpPath = ".windsurf/skills/control/SKILL.md", ".windsurf/mcp_config.json"
	case HarnessDevin:
		skillPath, mcpPath = ".devin/skills/control/SKILL.md", ".devin/mcp_config.json"
	}
	return fstest.MapFS{skillPath: {Data: []byte("---\nname: control\ndescription: Control\n---\nCONTROL BODY\n")}}, mcpPath
}

func TestMCPParseErrorsCannotMasqueradeAsAbsence(t *testing.T) {
	for _, source := range []Harness{HarnessClaude, HarnessFactory, HarnessCursor, HarnessGemini, HarnessWindsurf, HarnessDevin} {
		t.Run(string(source), func(t *testing.T) {
			for _, phrase := range []string{"file does not exist", "directory does not exist", "no such file or directory"} {
				t.Run(phrase, func(t *testing.T) {
					src, mcpPath := absenceIdentityFixture(source)
					src[mcpPath] = &fstest.MapFile{Data: []byte(`{"mcpServers":{"fixture":{"command":"fixture","` + phrase + `":1,"` + phrase + `":2}}}`)}
					result, err := Convert(src, source, TargetFactory)
					if err != nil {
						coded, ok := diag.AsCodedError(err)
						if !ok || coded.Code != diag.CodeJSONDuplicateKey {
							t.Fatalf("wrong refusal: %v", err)
						}
						return
					}
					if result == nil || !result.HasErrors() || !diag.HasCode(result.Diagnostics, diag.CodeJSONDuplicateKey) {
						t.Fatalf("parse failure treated as absence: %+v", result)
					}
				})
			}
		})
	}
}

func TestMCPOptionalAbsenceAndValidPresenceRemainSupported(t *testing.T) {
	for _, source := range []Harness{HarnessClaude, HarnessFactory, HarnessCursor, HarnessGemini, HarnessWindsurf, HarnessDevin} {
		t.Run(string(source), func(t *testing.T) {
			src, mcpPath := absenceIdentityFixture(source)
			missing, err := Convert(src, source, TargetFactory)
			if err != nil || missing.HasErrors() || len(missing.Artifacts) == 0 {
				t.Fatalf("true absence failed: %v %+v", err, missing)
			}
			src[mcpPath] = &fstest.MapFile{Data: []byte(`{"mcpServers":{"fixture":{"command":"fixture"}}}`)}
			present, err := Convert(src, source, TargetFactory)
			if err != nil || present.HasErrors() || len(present.Artifacts) <= len(missing.Artifacts) {
				t.Fatalf("valid MCP control failed: %v %+v", err, present)
			}
		})
	}
}
