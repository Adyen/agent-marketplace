package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	windsurfingest "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/windsurf"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func directWindsurfFixture() fstest.MapFS {
	return fstest.MapFS{
		"skills/native/SKILL.md": {Data: []byte("---\nname: native\ndescription: Native\n---\nSkill body\n")},
		"workflows/deploy.md":    {Data: []byte("---\ndescription: Deploy\n---\nWorkflow body\n")},
		"hooks.json":             {Data: []byte(`{"hooks":{"pre_run_command":[{"command":"guard-command"}]}}`)},
	}
}

func TestWindsurfDirectRootMetadataDoesNotHideResources(t *testing.T) {
	for _, metadata := range []string{"AGENTS.md", "mcp_config.json"} {
		t.Run(metadata, func(t *testing.T) {
			src := directWindsurfFixture()
			body := "# Handwritten agent notes\n"
			if metadata == "mcp_config.json" {
				body = `{"mcpServers":{"native-server":{"command":"fixture-server"}}}`
			}
			src[metadata] = &fstest.MapFile{Data: []byte(body)}
			bundle, err := (windsurfingest.Ingester{}).LoadBundle(src)
			if err != nil {
				t.Fatal(err)
			}
			kinds := map[string]int{}
			for _, r := range bundle.Resources {
				switch r.Kind() {
				case ir.KindSkill:
					kinds["skill"]++
				case ir.KindCommand:
					kinds["command"]++
				case ir.KindHook:
					kinds["hook"]++
				}
			}
			if kinds["skill"] != 1 || kinds["command"] != 1 || kinds["hook"] != 1 {
				t.Fatalf("direct root resources hidden: %v", kinds)
			}
			result, err := Convert(src, HarnessWindsurf, TargetWindsurf)
			if err != nil || result.HasErrors() {
				t.Fatalf("conversion failed: %v %+v", err, result)
			}
			seenSkill, seenCommand, seenHook := false, false, false
			for _, a := range result.Artifacts {
				body := string(a.Bytes)
				seenSkill = seenSkill || strings.Contains(body, "Skill body")
				seenCommand = seenCommand || strings.Contains(body, "Workflow body")
				seenHook = seenHook || strings.Contains(body, "guard-command")
			}
			if !seenSkill || !seenCommand || !seenHook {
				t.Fatalf("direct resource output omitted: %+v", result)
			}
		})
	}
}

func TestWindsurfRealWorkspaceKeepsPrecedenceAndLegacyMCP(t *testing.T) {
	src := directWindsurfFixture()
	src["AGENTS.md"] = &fstest.MapFile{Data: []byte("# notes\n")}
	src["mcp_config.json"] = &fstest.MapFile{Data: []byte(`{"mcpServers":{"legacy":{"command":"legacy-server"}}}`)}
	src[".windsurf/workflows/workspace.md"] = &fstest.MapFile{Data: []byte("---\ndescription: Workspace\n---\nWORKSPACE BODY\n")}
	result, err := Convert(src, HarnessWindsurf, TargetWindsurf)
	if err != nil || result.HasErrors() {
		t.Fatalf("workspace conversion failed: %v %+v", err, result)
	}
	workspace, legacy := false, false
	for _, a := range result.Artifacts {
		body := string(a.Bytes)
		workspace = workspace || strings.Contains(body, "WORKSPACE BODY")
		legacy = legacy || strings.Contains(body, "legacy-server")
		if strings.Contains(body, "Workflow body") {
			t.Fatal("direct root overrode real workspace")
		}
	}
	if !workspace || !legacy {
		t.Fatalf("workspace/legacy fallback lost: %+v", result)
	}
}
