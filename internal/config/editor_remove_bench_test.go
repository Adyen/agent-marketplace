package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func BenchmarkDisableResourceWithShadowFallbackExactDuplicates(b *testing.B) {
	const (
		otherRefs       = 256
		exactDuplicates = 8
		targetURL       = "git@example.com:shared.git"
		targetPath      = "teams/target"
		targetRef       = "release"
		resourceName    = "stale-resource"
	)

	var fixture strings.Builder
	fixture.WriteString("apiVersion: agent-marketplace/v2\nregistries:\n")
	for i := 0; i < otherRefs; i++ {
		fmt.Fprintf(&fixture, "  - url: %s\n    path: %s\n    ref: ref-%03d\n    skills:\n      include: [%s]\n", targetURL, targetPath, i, resourceName)
	}
	for i := 0; i < exactDuplicates; i++ {
		fmt.Fprintf(&fixture, "  - url: %s\n    path: %s\n    ref: %s\n    skills:\n      include: [%s, keep-%d]\n", targetURL, targetPath, targetRef, resourceName, i)
	}
	baseYAML := []byte(fixture.String())
	configPath := filepath.Join(b.TempDir(), "config.yaml")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := os.WriteFile(configPath, baseYAML, 0o644); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := DisableResourceWithShadowFallback(configPath, targetURL, targetPath, targetRef, ir.KindSkill, resourceName); err != nil {
			b.Fatal(err)
		}
	}
}
