package pluginpolicy_test

import (
	"strings"
	"testing"

	harnesscompile "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

var _ = harnesscompile.KindSkill // keep the root import that registers compilers

func memberDiagCodes(mp pluginpolicy.MemberProjection) []string {
	var codes []string
	for _, d := range mp.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

func findMember(p *pluginpolicy.Projection, key string) *pluginpolicy.MemberProjection {
	for i := range p.Members {
		if p.Members[i].Key == key {
			return &p.Members[i]
		}
	}
	return nil
}

// spec: plugin-delivery/claude-plugin-subagent-with-permissionmode-is-blocked
func TestProjectClaudeNativePermissionModeBlocked(t *testing.T) {
	sub := ir.NewSubagent("guard", "d", "body", "", nil, "", nil, "", map[string]any{"permissionMode": "plan"}, ir.SourceRef{})
	p := &ir.Plugin{
		Format:   "claude-plugin",
		Manifest: ir.Manifest{Name: "demo"},
		Members:  []ir.PluginMember{{Resource: sub}},
	}
	proj, err := pluginpolicy.Project(p, target.TargetClaude, pluginpolicy.DeliveryNative, compile.ScopeProject, pluginpolicy.Roots{})
	if err != nil {
		t.Fatal(err)
	}
	mp := findMember(proj, "subagent/guard")
	if mp == nil {
		t.Fatalf("members = %v, want subagent/guard", proj.Members)
	}
	if !mp.Blocked {
		t.Fatal("a permissionMode subagent delivered natively to Claude must be blocked")
	}
	if !proj.Blocked {
		t.Fatal("a blocked member must block the projection")
	}
	if len(mp.Artifacts) != 0 {
		t.Fatalf("a blocked member must emit no artifacts, got %d", len(mp.Artifacts))
	}
	found := false
	for _, d := range mp.Diagnostics {
		if d.Code == "plugin.member.restriction-unsupported" && d.IsError() {
			found = true
		}
	}
	if !found {
		t.Fatalf("want plugin.member.restriction-unsupported error, got %v", memberDiagCodes(*mp))
	}
}

// spec: plugin-delivery/claude-plugin-subagent-with-permissionmode-is-blocked
func TestProjectClaudeDecomposePermissionModeNotBlocked(t *testing.T) {
	sub := ir.NewSubagent("guard", "d", "body", "", nil, "", nil, "", map[string]any{"permissionMode": "plan"}, ir.SourceRef{})
	p := &ir.Plugin{
		Format:   "claude-plugin",
		Manifest: ir.Manifest{Name: "demo"},
		Members:  []ir.PluginMember{{Resource: sub}},
	}
	proj, err := pluginpolicy.Project(p, target.TargetClaude, pluginpolicy.DeliveryDecompose, compile.ScopeProject, pluginpolicy.Roots{})
	if err != nil {
		t.Fatal(err)
	}
	mp := findMember(proj, "subagent/guard")
	if mp == nil || mp.Blocked {
		t.Fatalf("decomposed delivery does not apply the plugin surface, member = %+v", mp)
	}
	if len(mp.Artifacts) == 0 {
		t.Fatal("the decomposed subagent must still compile to an artifact")
	}
}

// spec: plugin-delivery/unresolved-model-on-a-plugin-member-warns-and-installs
func TestProjectUnresolvedModelWarnsAndInstalls(t *testing.T) {
	sub := ir.NewSubagent("worker", "d", "body", "definitely-not-a-catalog-model", nil, "", nil, "", nil, ir.SourceRef{})
	p := &ir.Plugin{
		Format:   "claude-plugin",
		Manifest: ir.Manifest{Name: "demo"},
		Members:  []ir.PluginMember{{Resource: sub}},
	}
	proj, err := pluginpolicy.Project(p, target.TargetClaude, pluginpolicy.DeliveryNative, compile.ScopeProject, pluginpolicy.Roots{})
	if err != nil {
		t.Fatal(err)
	}
	mp := findMember(proj, "subagent/worker")
	if mp == nil {
		t.Fatalf("members = %v", proj.Members)
	}
	if mp.Blocked {
		t.Fatalf("an unresolved model is a preference loss, not a block: %v", memberDiagCodes(*mp))
	}
	if len(mp.Artifacts) != 1 {
		t.Fatalf("the member must still install without the model field, artifacts = %d", len(mp.Artifacts))
	}
	found := false
	for _, d := range mp.Diagnostics {
		if d.Code == "model.unresolved" && d.Severity == diag.SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a model.unresolved warning, got %v", memberDiagCodes(*mp))
	}
	if strings.Contains(string(mp.Artifacts[0].Bytes), "definitely-not-a-catalog-model") {
		t.Fatal("the unresolved model field must be omitted from the artifact")
	}
}

// spec: plugin-portable-format/plugin-root-expands-in-arguments-only
func TestProjectPluginRootExpandsInArgumentsOnly(t *testing.T) {
	srv := ir.NewMcpServer("srv", "${PLUGIN_ROOT}/bin/run", []string{"${PLUGIN_ROOT}/cfg.json"}, map[string]string{"K": "${PLUGIN_DATA}/v"}, nil, ir.SourceRef{})
	srv.Cwd = "${PLUGIN_ROOT}"
	p := &ir.Plugin{
		Format:   "agent-plugin",
		Manifest: ir.Manifest{Name: "demo"},
		Members:  []ir.PluginMember{{Resource: srv}},
	}
	roots := pluginpolicy.Roots{Package: "/gen/demo", Data: "/data/demo"}
	proj, err := pluginpolicy.Project(p, target.TargetClaude, pluginpolicy.DeliveryDecompose, compile.ScopeProject, roots)
	if err != nil {
		t.Fatal(err)
	}
	mp := findMember(proj, "mcpserver/srv")
	if mp == nil || mp.Blocked {
		t.Fatalf("member = %+v", mp)
	}
	if len(mp.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, diags = %v", len(mp.Artifacts), mp.Diagnostics)
	}
	body := string(mp.Artifacts[0].Bytes)
	if !strings.Contains(body, "/gen/demo/cfg.json") {
		t.Fatalf("args ${PLUGIN_ROOT} must expand to the package root, got %s", body)
	}
	if !strings.Contains(body, "/data/demo/v") {
		t.Fatalf("env ${PLUGIN_DATA} must expand to the data root, got %s", body)
	}
	if !strings.Contains(body, `"${PLUGIN_ROOT}/bin/run"`) {
		t.Fatalf("command must keep ${PLUGIN_ROOT} literal, got %s", body)
	}
}

func TestProjectDecomposeIgnoresNativeAccepts(t *testing.T) {
	skill := ir.NewSkill("s", "d", "body", nil, nil, ir.SourceRef{})
	p := &ir.Plugin{
		Format:   "claude-plugin",
		Manifest: ir.Manifest{Name: "demo"},
		Members:  []ir.PluginMember{{Resource: skill}},
	}
	for _, tgt := range []target.Target{target.TargetCursor, target.TargetGemini} {
		proj, err := pluginpolicy.Project(p, tgt, pluginpolicy.DeliveryDecompose, compile.ScopeProject, pluginpolicy.Roots{})
		if err != nil {
			t.Fatalf("decomposed delivery to %s must not apply the native accepts list: %v", tgt, err)
		}
		mp := findMember(proj, "skill/s")
		if mp == nil || mp.Blocked {
			t.Fatalf("member to %s = %+v", tgt, mp)
		}
		if len(mp.Artifacts) == 0 {
			t.Fatalf("decomposed member to %s must produce artifacts", tgt)
		}
	}
	if _, err := pluginpolicy.Project(p, target.TargetGemini, pluginpolicy.DeliveryNative, compile.ScopeProject, pluginpolicy.Roots{}); err == nil {
		t.Fatal("native delivery to a target whose accepts list lacks the format must still refuse")
	}
}

func TestProjectUnrecognizedDeliveryIsPlainError(t *testing.T) {
	skill := ir.NewSkill("s", "d", "body", nil, nil, ir.SourceRef{})
	p := &ir.Plugin{
		Format:   "claude-plugin",
		Manifest: ir.Manifest{Name: "demo"},
		Members:  []ir.PluginMember{{Resource: skill}},
	}
	_, err := pluginpolicy.Project(p, target.TargetClaude, pluginpolicy.Delivery("bogus"), compile.ScopeProject, pluginpolicy.Roots{})
	if err == nil {
		t.Fatal("an unrecognized delivery must fail")
	}
	if strings.Contains(err.Error(), "plugin.format.unsupported") {
		t.Fatalf("a caller argument error must not carry a package-format code, got %v", err)
	}
}

// spec: plugin-portable-format/unrecognized-placeholder-stays-literal
func TestProjectUnrecognizedPlaceholderLiteralOrBlocked(t *testing.T) {
	srv := ir.NewMcpServer("srv", "run", []string{"${UNRECOGNIZED}"}, nil, nil, ir.SourceRef{})
	p := &ir.Plugin{
		Format:   "agent-plugin",
		Manifest: ir.Manifest{Name: "demo"},
		Members:  []ir.PluginMember{{Resource: srv}},
	}
	proj, err := pluginpolicy.Project(p, target.TargetDevin, pluginpolicy.DeliveryDecompose, compile.ScopeProject, pluginpolicy.Roots{})
	if err != nil {
		t.Fatal(err)
	}
	mp := findMember(proj, "mcpserver/srv")
	if mp == nil {
		t.Fatalf("members = %v", proj.Members)
	}
	if mp.Blocked {
		found := false
		for _, d := range mp.Diagnostics {
			if d.Code == "plugin.member.literal-unrepresentable" {
				found = true
			}
		}
		if !found {
			t.Fatalf("a blocked literal placeholder must carry plugin.member.literal-unrepresentable, got %v", memberDiagCodes(*mp))
		}
		return
	}
	for _, a := range mp.Artifacts {
		if !strings.Contains(string(a.Bytes), "${UNRECOGNIZED}") {
			t.Fatalf("the literal placeholder must survive unchanged, got %s", a.Bytes)
		}
	}
}
