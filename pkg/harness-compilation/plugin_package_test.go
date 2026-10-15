package harnesscompile_test

import (
	"errors"
	"strings"
	"testing"

	harnesscompile "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestCompileRefusesPlugin(t *testing.T) {
	p := &ir.Plugin{Manifest: ir.Manifest{Name: "demo"}}
	_, err := harnesscompile.Compile(p, harnesscompile.TargetClaude)
	if err == nil {
		t.Fatal("Compile must refuse a plugin resource")
	}
	if !strings.Contains(err.Error(), "plugin.compile.package-required") {
		t.Fatalf("want plugin.compile.package-required, got %v", err)
	}
	var coded *diag.CodedError
	if !errors.As(err, &coded) || coded.Code != "plugin.compile.package-required" {
		t.Fatalf("want a coded plugin.compile.package-required error, got %v", err)
	}
	// The inner compile.Compile boundary refuses identically.
	if _, err := compile.Compile(p, compile.TargetClaude); err == nil || !strings.Contains(err.Error(), "plugin.compile.package-required") {
		t.Fatalf("compile.Compile must refuse a plugin with plugin.compile.package-required, got %v", err)
	}
}

func TestCompilePackageProjects(t *testing.T) {
	skill := ir.NewSkill("ok", "fine skill", "Do the thing.", nil, nil, ir.SourceRef{})
	p := &ir.Plugin{
		Format:   "claude-plugin",
		Manifest: ir.Manifest{Name: "demo"},
		Members:  []ir.PluginMember{{Resource: skill}},
	}
	proj, err := harnesscompile.CompilePackage(p, harnesscompile.TargetClaude,
		pluginpolicy.DeliveryDecompose, harnesscompile.ScopeProject, pluginpolicy.Roots{})
	if err != nil {
		t.Fatal(err)
	}
	if proj.Plugin != "demo" || proj.Target != target.TargetClaude || proj.Delivery != pluginpolicy.DeliveryDecompose {
		t.Fatalf("projection identity = %q %q %q", proj.Plugin, proj.Target, proj.Delivery)
	}
	if len(proj.Members) != 1 || proj.Members[0].Key != "skill/ok" {
		t.Fatalf("members = %+v", proj.Members)
	}
	if len(proj.Members[0].Artifacts) != 1 || proj.Members[0].Artifacts[0].Path != "skills/ok/SKILL.md" {
		t.Fatalf("artifacts = %+v", proj.Members[0].Artifacts)
	}
}
