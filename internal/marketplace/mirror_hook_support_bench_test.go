package marketplace

import (
	"strings"
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// BenchmarkStageMirrorSupportFiles includes descriptor validation, hashing, and
// cache filesystem writes, but no network. Skill staging is the existing-path
// control for the added hook integrity validation.
func BenchmarkStageMirrorSupportFiles(b *testing.B) {
	file := hccartifact.SupportFile{Data: []byte(strings.Repeat("support\n", 512)), Executable: true}
	skill := ir.NewSkill("skill", "support", "body", nil, nil, ir.SourceRef{})
	skill.SupportFiles = []string{"run.sh"}
	hook := ir.NewHook("hook", "PreToolUse", "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh", "command", nil, ir.SourceRef{})
	hook.Command = "echo support"
	hook.SupportFiles = map[string]string{"run.sh": hccartifact.SupportFileDigest(file)}
	ca := hccartifact.CanonicalArtifact{Support: map[string]hccartifact.SupportFile{"run.sh": file}}
	for _, resource := range []ir.Resource{skill, hook} {
		b.Run(resource.Kind().String(), func(b *testing.B) {
			cache := NewCache(b.TempDir())
			digest := "sha256:" + strings.Repeat("a", 64)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := stageMirrorResource(cache, digest, resource, ca); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
