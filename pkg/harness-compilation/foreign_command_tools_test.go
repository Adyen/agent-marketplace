package harnesscompile

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func foreignToolCommandBytes(t *testing.T, artifacts []Artifact, name string) string {
	t.Helper()
	for _, a := range artifacts {
		if strings.Contains(a.Path, "commands/"+name+".") {
			return string(a.Bytes)
		}
	}
	t.Fatalf("command %q not emitted: %+v", name, artifacts)
	return ""
}

func foreignToolDiag(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Code == diag.CodeSkillAllowedToolsOmitted {
			return true
		}
	}
	return false
}

var foreignCommandSources = map[Harness]fstest.MapFS{
	HarnessDevin:   {".devin/skills/deploy/SKILL.md": {Data: []byte("---\nname: deploy\ndescription: Deploy\nx-agent-marketplace-kind: command\nallowed-tools:\n  - read\n  - exec\n---\nInert.\n")}},
	HarnessFactory: {".factory/commands/deploy.md": {Data: []byte("---\ndescription: Deploy\nallowed-tools:\n  - Read\n  - Edit\n---\nInert.\n")}},
}

// spec: harness-compilation/devin-and-factory-command-tool-lists-are-omitted-on-pre-approval-targets
func TestForeignCommandToolListsAreOmittedOnPreApprovalTargets(t *testing.T) {
	for from, src := range foreignCommandSources {
		for _, to := range []Target{TargetClaude, TargetCursor, TargetGemini} {
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				result, err := Convert(src, from, to)
				if err != nil {
					t.Fatal(err)
				}
				if out := foreignToolCommandBytes(t, result.Artifacts, "deploy"); strings.Contains(out, "allowed-tools") {
					t.Fatalf("foreign command tool list emitted as a pre-approval:\n%s", out)
				}
				if !foreignToolDiag(result.Diagnostics) {
					t.Fatalf("missing %s: %+v", diag.CodeSkillAllowedToolsOmitted, result.Diagnostics)
				}
			})
		}
		t.Run(string(from)+"/marketplace-payload", func(t *testing.T) {
			bundle, err := IngestBundle(src, from)
			if err != nil {
				t.Fatal(err)
			}
			var command *ir.Command
			for _, r := range bundle.Resources {
				if c, ok := r.(*ir.Command); ok {
					command = c
				}
			}
			if command == nil {
				t.Fatalf("no command ingested: %+v", bundle.Resources)
			}
			canonical, err := artifact.FromIR(command)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := artifact.DecodeResource("command", canonical.Payload)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := Compile(decoded, TargetClaude)
			if err != nil {
				t.Fatal(err)
			}
			if out := foreignToolCommandBytes(t, compiled.Artifacts, "deploy"); strings.Contains(out, "allowed-tools") || strings.Contains(out, "ToolSource") {
				t.Fatalf("payload round trip emitted a foreign tool list or provenance:\n%s", out)
			}
			if !foreignToolDiag(compiled.Diagnostics) {
				t.Fatalf("missing %s after payload round trip: %+v", diag.CodeSkillAllowedToolsOmitted, compiled.Diagnostics)
			}
		})
	}
}

// spec: harness-compilation/canonical-and-same-harness-command-tool-lists-are-unchanged
func TestCanonicalAndSameHarnessCommandToolListsAreUnchanged(t *testing.T) {
	for _, tc := range []struct {
		from Harness
		to   Target
		src  fstest.MapFS
		want string
	}{
		{HarnessClaude, TargetClaude, fstest.MapFS{"commands/deploy.md": {Data: []byte("---\ndescription: Deploy\nallowed-tools:\n  - Read\n---\nInert.\n")}}, "allowed-tools:\n  - Read\n"},
		{HarnessDevin, TargetDevin, fstest.MapFS{".devin/skills/deploy/SKILL.md": {Data: []byte("---\nname: deploy\ndescription: Deploy\nx-agent-marketplace-kind: command\nallowed-tools:\n  - read\n---\nInert.\n")}}, "allowed-tools:\n  - read\n"},
	} {
		t.Run(string(tc.from), func(t *testing.T) {
			result, err := Convert(tc.src, tc.from, tc.to)
			if err != nil {
				t.Fatal(err)
			}
			var out string
			for _, a := range result.Artifacts {
				if strings.Contains(a.Path, "deploy") {
					out += string(a.Bytes)
				}
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("tool list changed, want %q in:\n%s", tc.want, out)
			}
			if foreignToolDiag(result.Diagnostics) {
				t.Fatalf("unexpected %s", diag.CodeSkillAllowedToolsOmitted)
			}
		})
	}
}
