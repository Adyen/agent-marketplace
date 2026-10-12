package materialize

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func previewSkill(name string, machine bool) resolver.ResolvedArtifact {
	art := cleanupLinkSkill(name, machine, hcc.TargetDevin)
	art.RequiredBy = []resolver.ResourceRef{{Kind: ir.KindSkill, Name: "parent"}}
	return art
}

func previewMCP(name string) resolver.ResolvedArtifact {
	art := mcpArtifact(name, false, hcc.TargetDevin)
	art.RequiredBy = []resolver.ResourceRef{{Kind: ir.KindSkill, Name: "parent"}}
	return art
}

func mcpArtifact(name string, machine bool, targets ...hcc.Target) resolver.ResolvedArtifact {
	source := config.RepoCommitted
	if machine {
		source = config.UserPersonal
	}
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: name, Kind: ir.KindMcpServer, Resource: ir.NewMcpServer(name, name+"-binary", nil, nil, nil, ir.SourceRef{})},
		Source:    source,
		Harnesses: targets,
	}
}

func requiredMCP(name string, machine bool, targets ...hcc.Target) resolver.ResolvedArtifact {
	art := mcpArtifact(name, machine, targets...)
	art.RequiredBy = []resolver.ResourceRef{{Kind: ir.KindSkill, Name: "parent"}}
	return art
}

func runPreview(t *testing.T, repo string, resolved, preview []resolver.ResolvedArtifact, opts ...RunOption) *Report {
	t.Helper()
	opts = append(opts, WithImpliedPreview(preview))
	report, err := Run(context.Background(), repo, resolved, opts...)
	if err != nil {
		t.Fatalf("Run(preview) error = %v", err)
	}
	return report
}

// spec: materialization/preview-preserves-existing-owned-file-and-link-paths
func TestImpliedPreviewPreservesExistingSkillLink(t *testing.T) {
	repo, _ := devinEnv(t)
	helper := previewSkill("helper", false)
	seedCleanupLinks(t, repo, helper)
	link := filepath.Join(repo, ".devin", "skills", "helper")
	targetBefore, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	statBefore, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}

	selected := cleanupLinkSkill("parent", false, hcc.TargetDevin)
	runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{helper})

	targetAfter, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("preview output was not retained: %v", err)
	}
	if targetAfter != targetBefore {
		t.Fatalf("preview link retargeted: %q -> %q", targetBefore, targetAfter)
	}
	if statAfter, err := os.Lstat(link); err != nil || statAfter.ModTime() != statBefore.ModTime() {
		t.Fatalf("preview link rewritten: %v", err)
	}
	missing := previewSkill("missing", false)
	runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{helper, missing})
	if _, err := os.Lstat(filepath.Join(repo, ".devin", "skills", "missing")); !os.IsNotExist(err) {
		t.Fatalf("preview created a missing output: %v", err)
	}
	runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{helper}, WithDryRun())
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("dry-run preview dropped the retained output: %v", err)
	}
}

// spec: materialization/preview-preserves-existing-aggregate-identity-without-adoption
func TestImpliedPreviewPreservesAggregateIdentityWithoutAdoption(t *testing.T) {
	repo, _ := devinEnv(t)
	keep := previewMCP("keep")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{keep})
	if err != nil || report == nil || len(report.Diags) != 0 {
		t.Fatalf("seed = %#v err=%v", report, err)
	}
	path := filepath.Join(devinProjectRoot(repo), "mcp_config.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	handwritten := `{"mcpServers":{"handwritten":{"command":"manual"},"keep":{"command":"keep-binary"}}}`
	if err := os.WriteFile(path, []byte(handwritten), 0o600); err != nil {
		t.Fatal(err)
	}

	selected := cleanupLinkSkill("parent", false, hcc.TargetDevin)
	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep})
	if report != nil && hasErrorDiagnostics(report.Diags) {
		t.Fatalf("preview Run diagnostics = %#v", report.Diags)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != handwritten {
		t.Fatalf("preview rewrote the aggregate:\nbefore %s\nafter  %s", before, after)
	}
	other := previewMCP("other")
	runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep, other})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != handwritten {
		t.Fatalf("preview-only identity materialized into the aggregate: %s", raw)
	}
}

// spec: materialization/preview-retention-does-not-suppress-unrelated-pruning
func TestImpliedPreviewKeepsUnrelatedStalePruning(t *testing.T) {
	repo, _ := devinEnv(t)
	keep := previewSkill("keep", false)
	stale := cleanupLinkSkill("stale", false, hcc.TargetDevin)
	seedCleanupLinks(t, repo, keep, stale)

	selected := cleanupLinkSkill("parent", false, hcc.TargetDevin)
	runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep})

	if _, err := os.Lstat(filepath.Join(repo, ".devin", "skills", "keep")); err != nil {
		t.Fatalf("preview output pruned: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".devin", "skills", "stale")); !os.IsNotExist(err) {
		t.Fatalf("unrelated stale output retained: %v", err)
	}
}

// spec: materialization/removing-the-last-requiring-selection-ends-preview-retention
func TestImpliedPreviewRetentionEndsWithLastRequirer(t *testing.T) {
	repo, _ := devinEnv(t)
	keep := previewSkill("keep", false)
	seedCleanupLinks(t, repo, keep)
	selected := cleanupLinkSkill("parent", false, hcc.TargetDevin)

	runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep})
	if _, err := os.Lstat(filepath.Join(repo, ".devin", "skills", "keep")); err != nil {
		t.Fatalf("preview output pruned while still required: %v", err)
	}

	runPreview(t, repo, []resolver.ResolvedArtifact{selected}, nil)
	if _, err := os.Lstat(filepath.Join(repo, ".devin", "skills", "keep")); !os.IsNotExist(err) {
		t.Fatalf("output retained after the last requirer disappeared: %v", err)
	}
}

// spec: materialization/preview-preservation-does-not-weaken-fail-closed-ownership-or-aggregate-handling
func TestImpliedPreviewMalformedAggregateFailsClosed(t *testing.T) {
	repo, _ := devinEnv(t)
	keep := previewMCP("keep")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{keep})
	if err != nil || report == nil || len(report.Diags) != 0 {
		t.Fatalf("seed = %#v err=%v", report, err)
	}
	path := filepath.Join(devinProjectRoot(repo), "mcp_config.json")
	malformed := []byte("{ not json")
	if err := os.WriteFile(path, malformed, 0o600); err != nil {
		t.Fatal(err)
	}

	selected := cleanupLinkSkill("parent", false, hcc.TargetDevin)
	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep})
	if report == nil || !hasErrorDiagnostics(report.Diags) {
		t.Fatalf("malformed aggregate under preview must fail closed, diags = %#v", report.Diags)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(malformed) {
		t.Fatalf("fail-closed run rewrote the malformed aggregate: %s", raw)
	}
}

// spec: materialization/preview-preservation-does-not-weaken-fail-closed-ownership-or-aggregate-handling
func TestImpliedPreviewOwnershipStateFailClosed(t *testing.T) {
	for _, mutation := range []string{"missing", "corrupt", "healthy"} {
		t.Run(mutation, func(t *testing.T) {
			repo, _ := devinEnv(t)
			keep := previewMCP("keep")
			sibling := mcpArtifact("sibling", false, hcc.TargetDevin)
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{keep, sibling})
			if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("seed=%#v err=%v", report, err)
			}
			path := filepath.Join(devinProjectRoot(repo), "mcp_config.json")
			before := readFileString(t, path)

			store, err := ownership.Open(devinProjectRoot(repo))
			if err != nil {
				t.Fatal(err)
			}
			stateFile := store.StateFile()
			var retained os.FileInfo
			switch mutation {
			case "missing":
				if retained, err = os.Lstat(stateFile); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(stateFile); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(stateFile, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			selected := cleanupLinkSkill("parent", false, hcc.TargetDevin)
			report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{selected, sibling}, WithImpliedPreview([]resolver.ResolvedArtifact{keep}))
			if err != nil {
				t.Fatalf("Run error = %v", err)
			}
			switch mutation {
			case "missing":
				if !hasDiagCode(report.Diags, diag.CodeOwnershipStateMissing) {
					t.Fatalf("missing ownership produced no state-missing diagnostic: %#v", report.Diags)
				}
				if hasErrorDiagnostics(report.Diags) {
					t.Fatalf("missing ownership under preview became an error: %#v", report.Diags)
				}
			case "corrupt":
				if !hasDiagCode(report.Diags, diag.CodeOwnershipStateCorrupt) {
					t.Fatalf("corrupt ownership produced no state-corrupt diagnostic: %#v", report.Diags)
				}
			case "healthy":
				requireNoErrorDiags(t, report.Diags)
			}
			if got := readFileString(t, path); got != before {
				t.Fatalf("%s ownership rewrote the aggregate:\nbefore %s\nafter  %s", mutation, before, got)
			}
			if mutation != "healthy" {
				proof, proofErr := store.Proof(harness.DevinMcpConfigFile)
				if proofErr == nil {
					if _, ok := proof.Entry(ownership.EntryID{Kind: ownership.KindMcpServer, Container: "mcpServers", Key: "keep", IdentityDigest: ownership.McpIdentityDigest("keep")}); ok {
						t.Fatalf("%s ownership inferred an ownership record for the preview entry", mutation)
					}
				}
			}
			if mutation == "missing" {
				if info, statErr := os.Lstat(stateFile); retained != nil && statErr == nil && os.SameFile(info, retained) {
					t.Fatal("missing-state run resurrected the removed record")
				}
			}
		})
	}
}

// spec: materialization/preview-retention-does-not-suppress-unrelated-pruning
func TestImpliedPreviewRetainsKeyedEntryAcrossSiblingUpdate(t *testing.T) {
	repo, _ := devinEnv(t)
	keep := previewMCP("aalpha")
	stale := mcpArtifact("zzstale", false, hcc.TargetDevin)
	sibling := mcpArtifact("beta", false, hcc.TargetDevin)
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{keep, stale, sibling})
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed=%#v err=%v", report, err)
	}
	path := filepath.Join(devinProjectRoot(repo), "mcp_config.json")
	before := readFileString(t, path)

	mcpID := func(name string) ownership.EntryID {
		return ownership.EntryID{Kind: ownership.KindMcpServer, Container: "mcpServers", Key: name, IdentityDigest: ownership.McpIdentityDigest(name)}
	}
	proofFor := func(t *testing.T) ownership.Proof {
		t.Helper()
		store, err := ownership.Open(devinProjectRoot(repo))
		if err != nil {
			t.Fatal(err)
		}
		proof, err := store.Proof(harness.DevinMcpConfigFile)
		if err != nil {
			t.Fatal(err)
		}
		return proof
	}
	seedProof := proofFor(t)
	seedAlpha, ok := seedProof.Entry(mcpID("aalpha"))
	if !ok {
		t.Fatal("seed produced no ownership record for aalpha")
	}
	seedBeta, ok := seedProof.Entry(mcpID("beta"))
	if !ok {
		t.Fatal("seed produced no ownership record for beta")
	}

	selected := cleanupLinkSkill("parent", false, hcc.TargetDevin)
	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep})
	requireNoErrorDiags(t, report.Diags)
	after := readFileString(t, path)
	if !strings.Contains(after, `"aalpha"`) {
		t.Fatalf("preview-retained entry lost during sibling update:\nbefore %s\nafter  %s", before, after)
	}
	if strings.Contains(after, `"zzstale"`) {
		t.Fatalf("unrelated stale entry survived preview retention: %s", after)
	}
	prunedProof := proofFor(t)

	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep})
	requireNoErrorDiags(t, report.Diags)
	noopProof := proofFor(t)
	if noopProof.Generation() != prunedProof.Generation() {
		t.Fatalf("preview-only no-op advanced generation %d -> %d", prunedProof.Generation(), noopProof.Generation())
	}
	noopAlpha, ok := noopProof.Entry(mcpID("aalpha"))
	if !ok {
		t.Fatal("preview no-op dropped the aalpha ownership record")
	}
	if !reflect.DeepEqual(noopAlpha, seedAlpha) {
		t.Fatalf("preview no-op rewrote the retained entry record:\nseed %+v\nnoop %+v", seedAlpha, noopAlpha)
	}

	joined := strings.Replace(after, `"mcpServers": {`, `"mcpServers": {"hand":{"command":"hand-binary"},`, 1)
	if joined == after {
		t.Fatalf("handwritten splice anchor missing in %s", after)
	}
	if err := os.WriteFile(path, []byte(joined), 0o600); err != nil {
		t.Fatal(err)
	}
	hand := previewMCP("hand")
	updated := mcpArtifact("beta", false, hcc.TargetDevin)
	updated.Resource = ir.NewMcpServer("beta", "beta-v2-binary", nil, nil, nil, ir.SourceRef{})
	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected, updated}, []resolver.ResolvedArtifact{keep, hand})
	requireNoErrorDiags(t, report.Diags)
	raw := readFileString(t, path)
	for _, want := range []string{`"aalpha"`, `"beta-v2-binary"`, `"hand"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("sibling update lost %s: %s", want, raw)
		}
	}
	if strings.Contains(raw, `"beta-binary"`) || strings.Contains(raw, `"zzstale"`) {
		t.Fatalf("sibling update kept stale content: %s", raw)
	}
	updatedProof := proofFor(t)
	updatedAlpha, ok := updatedProof.Entry(mcpID("aalpha"))
	if !ok {
		t.Fatal("sibling update dropped the aalpha ownership record")
	}
	if !reflect.DeepEqual(updatedAlpha, seedAlpha) {
		t.Fatalf("sibling update rewrote the retained entry evidence:\nseed    %+v\nupdated %+v", seedAlpha, updatedAlpha)
	}
	updatedBeta, ok := updatedProof.Entry(mcpID("beta"))
	if !ok || updatedBeta.ValueDigest == seedBeta.ValueDigest {
		t.Fatalf("real sibling content update recorded no new evidence: before %+v after %+v", seedBeta, updatedBeta)
	}
	if _, ok := updatedProof.Entry(mcpID("hand")); ok {
		t.Fatal("handwritten identical preview key was adopted into ownership")
	}

	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected, updated}, nil)
	requireNoErrorDiags(t, report.Diags)
	raw = readFileString(t, path)
	if strings.Contains(raw, `"aalpha"`) {
		t.Fatalf("retained entry lost its ownership proof and survived pruning: %s", raw)
	}
	if !strings.Contains(raw, `"hand"`) || !strings.Contains(raw, `"beta-v2-binary"`) {
		t.Fatalf("ordinary sync lost unmanaged or selected content: %s", raw)
	}
}

// spec: materialization/preview-preserves-existing-aggregate-identity-without-adoption
func TestImpliedPreviewDriftedOwnedEntryKeepsExistingBytes(t *testing.T) {
	repo, _ := devinEnv(t)
	keep := previewMCP("keep")
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{keep})
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed=%#v err=%v", report, err)
	}
	path := filepath.Join(devinProjectRoot(repo), "mcp_config.json")
	drifted := strings.Replace(readFileString(t, path), `"keep-binary"`, `"hand-edited"`, 1)
	if err := os.WriteFile(path, []byte(drifted), 0o600); err != nil {
		t.Fatal(err)
	}

	selected := cleanupLinkSkill("parent", false, hcc.TargetDevin)
	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep})
	requireNoErrorDiags(t, report.Diags)
	raw := readFileString(t, path)
	if !strings.Contains(raw, `"hand-edited"`) {
		t.Fatalf("preview overwrote drifted bytes with the desired copy: %s", raw)
	}
	runPreview(t, repo, []resolver.ResolvedArtifact{selected}, nil)
	if raw := readFileString(t, path); strings.Contains(raw, `"keep"`) {
		t.Fatalf("drifted preview entry survived ordinary pruning: %s", raw)
	}
}

// spec: materialization/preview-preserves-existing-aggregate-identity-without-adoption
func TestImpliedPreviewCodexMCPKeepsOwnedTOMLIdentity(t *testing.T) {
	repo, home := devinEnv(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	keep := requiredMCP("keep", false, hcc.TargetCodex)
	stale := mcpArtifact("stale", false, hcc.TargetCodex)
	machineKeep := requiredMCP("machine-keep", true, hcc.TargetCodex)
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{keep, stale, machineKeep})
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed=%#v err=%v", report, err)
	}
	repoConfig := filepath.Join(repo, ".codex", "config.toml")
	machineConfig := filepath.Join(home, ".codex", "config.toml")
	for _, path := range []string{repoConfig, machineConfig} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("seed config missing at %s: %v", path, err)
		}
	}

	selected := cleanupLinkSkill("parent", false, hcc.TargetCodex)
	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep, machineKeep})
	requireNoErrorDiags(t, report.Diags)
	repoRaw := readFileString(t, repoConfig)
	if !strings.Contains(repoRaw, `mcp_servers."keep"`) {
		t.Fatalf("repo-scope preview identity pruned from config.toml:\n%s", repoRaw)
	}
	if strings.Contains(repoRaw, `mcp_servers."stale"`) {
		t.Fatalf("unrelated stale TOML entry survived preview retention:\n%s", repoRaw)
	}
	if raw := readFileString(t, machineConfig); !strings.Contains(raw, `mcp_servers."machine-keep"`) {
		t.Fatalf("machine-scope preview identity pruned from CODEX_HOME config.toml:\n%s", raw)
	}
	runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{requiredMCP("absent", false, hcc.TargetCodex), keep})
	if raw := readFileString(t, repoConfig); strings.Contains(raw, `mcp_servers."absent"`) {
		t.Fatalf("preview wrote a new TOML entry:\n%s", raw)
	}
	runPreview(t, repo, []resolver.ResolvedArtifact{selected}, nil)
	if raw := readFileString(t, repoConfig); strings.Contains(raw, `mcp_servers."keep"`) {
		t.Fatalf("retained TOML entry lost ownership proof:\n%s", raw)
	}
}

// spec: materialization/preview-preserves-existing-aggregate-identity-without-adoption
func TestImpliedPreviewGeminiNativeRetainsOwnedMCP(t *testing.T) {
	repo, home := devinEnv(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	keep := requiredMCP("keep", true, hcc.TargetGemini)
	stale := mcpArtifact("stale", true, hcc.TargetGemini)
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{keep, stale})
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed=%#v err=%v", report, err)
	}
	settings := filepath.Join(home, ".gemini", "settings.json")
	if _, err := os.Stat(settings); err != nil {
		t.Fatalf("seed settings missing: %v", err)
	}
	selected := cleanupLinkSkill("parent", true, hcc.TargetGemini)
	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep})
	requireNoErrorDiags(t, report.Diags)
	raw := readFileString(t, settings)
	if !strings.Contains(raw, `"keep"`) {
		t.Fatalf("preview identity pruned from shared Gemini settings:\n%s", raw)
	}
	if strings.Contains(raw, `"stale"`) {
		t.Fatalf("unrelated stale entry survived preview retention:\n%s", raw)
	}
}

// spec: materialization/preview-retention-does-not-suppress-unrelated-pruning
func TestImpliedPreviewMarkerMergeRetainsManagedEntries(t *testing.T) {
	repo, _ := devinEnv(t)
	keep := requiredMCP("keep", false, hcc.TargetFactory)
	stale := mcpArtifact("stale", false, hcc.TargetFactory)
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{keep, stale})
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed=%#v err=%v", report, err)
	}
	path := filepath.Join(repo, ".factory", "mcp.json")
	managed := readFileString(t, path)
	if !strings.Contains(managed, `"keep"`) || !strings.Contains(managed, `"stale"`) {
		t.Fatalf("seed mcp.json missing managed entries: %s", managed)
	}

	selected := cleanupLinkSkill("parent", false, hcc.TargetFactory)
	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep})
	requireNoErrorDiags(t, report.Diags)
	raw := readFileString(t, path)
	if !strings.Contains(raw, `"keep"`) {
		t.Fatalf("uncovered prune dropped the preview-managed entry: %s", raw)
	}
	if strings.Contains(raw, `"stale"`) {
		t.Fatalf("unrelated stale managed entry survived: %s", raw)
	}

	sibling := mcpArtifact("sibling", false, hcc.TargetFactory)
	absent := requiredMCP("absent", false, hcc.TargetFactory)
	report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{keep, sibling})
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("reseed=%#v err=%v", report, err)
	}
	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected, sibling}, []resolver.ResolvedArtifact{keep, absent})
	requireNoErrorDiags(t, report.Diags)
	raw = readFileString(t, path)
	for _, want := range []string{`"keep"`, `"sibling"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("covered merge lost %s: %s", want, raw)
		}
	}
	if strings.Contains(raw, `"absent"`) {
		t.Fatalf("covered merge adopted an absent preview member: %s", raw)
	}
}

// spec: materialization/preview-preservation-does-not-weaken-fail-closed-ownership-or-aggregate-handling
func TestImpliedPreviewMergeSnapshotRefusesUnsafeAggregate(t *testing.T) {
	if os.Getenv("AM_TEST_IMPLIED_PREVIEW_FIFO") != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestImpliedPreviewMergeSnapshotRefusesUnsafeAggregate$")
		cmd.Env = append(os.Environ(), "AM_TEST_IMPLIED_PREVIEW_FIFO=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bounded implied-preview FIFO probe: %v\n%s", err, output)
		}
		return
	}
	for _, mutation := range []string{"fifo", "hardlink", "external-alias", "ordinary", "contained-alias"} {
		t.Run(mutation, func(t *testing.T) {
			repo, _ := devinEnv(t)
			keep := requiredMCP("keep", false, hcc.TargetFactory)
			sibling := mcpArtifact("sibling", false, hcc.TargetFactory)
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{keep, sibling})
			if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("seed=%#v err=%v", report, err)
			}
			path := filepath.Join(repo, ".factory", "mcp.json")
			seeded := readFileString(t, path)
			seededInfo, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}

			external := t.TempDir()
			externalPath := filepath.Join(external, "external.json")
			if err := os.WriteFile(externalPath, []byte(seeded), 0o640); err != nil {
				t.Fatal(err)
			}
			externalInfo, err := os.Lstat(externalPath)
			if err != nil {
				t.Fatal(err)
			}
			extraLink := filepath.Join(repo, "mcp-extra-link.json")
			aliasTarget := filepath.Join(repo, "alias-target.json")
			var fifoInfo os.FileInfo
			switch mutation {
			case "fifo":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(path, 0o640); err != nil {
					t.Fatal(err)
				}
				if fifoInfo, err = os.Lstat(path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, extraLink); err != nil {
					t.Fatal(err)
				}
			case "external-alias":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(externalPath, path); err != nil {
					t.Fatal(err)
				}
			case "contained-alias":
				if err := os.WriteFile(aliasTarget, []byte(seeded), 0o640); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(aliasTarget, path); err != nil {
					t.Fatal(err)
				}
			}

			selected := cleanupLinkSkill("parent", false, hcc.TargetFactory)
			report, err = Run(context.Background(), repo, []resolver.ResolvedArtifact{selected, sibling}, WithImpliedPreview([]resolver.ResolvedArtifact{keep}))
			if err != nil {
				t.Fatalf("Run error = %v", err)
			}
			switch mutation {
			case "fifo", "hardlink":
				if !hasDiagCode(report.Diags, "materialize.write.error") {
					t.Fatalf("%s aggregate produced no coded refusal, diags = %#v", mutation, report.Diags)
				}
				found := false
				for _, d := range report.Diags {
					if strings.Contains(d.Message, "unsafe file") {
						found = true
					}
				}
				if !found {
					t.Fatalf("refusal lost its unsafe-file identity, diags = %#v", report.Diags)
				}
			case "external-alias":
				if !hasDiagCode(report.Diags, "materialize.write.error") {
					t.Fatalf("external alias produced no coded refusal, diags = %#v", report.Diags)
				}
				found := false
				for _, d := range report.Diags {
					if strings.Contains(d.Message, "unsafe path") {
						found = true
					}
				}
				if !found {
					t.Fatalf("refusal lost its unsafe-path identity, diags = %#v", report.Diags)
				}
				text, linkErr := os.Readlink(path)
				if linkErr != nil || text != externalPath {
					t.Fatalf("refused alias changed: %q %v", text, linkErr)
				}
			case "ordinary", "contained-alias":
				requireNoErrorDiags(t, report.Diags)
				raw := readFileString(t, path)
				for _, want := range []string{`"keep"`, `"sibling"`} {
					if !strings.Contains(raw, want) {
						t.Fatalf("merge lost %s: %s", want, raw)
					}
				}
				if mutation == "contained-alias" {
					if info, statErr := os.Lstat(path); statErr != nil || info.Mode()&os.ModeSymlink != 0 {
						t.Fatalf("contained alias was not replaced by a regular file: %v", statErr)
					}
					if got := readFileString(t, aliasTarget); got != seeded {
						t.Fatalf("contained alias target changed: %s", got)
					}
				}
			}
			if mutation == "fifo" {
				info, statErr := os.Lstat(path)
				if statErr != nil || info.Mode()&os.ModeNamedPipe == 0 || !os.SameFile(info, fifoInfo) || info.Mode() != fifoInfo.Mode() {
					t.Fatalf("refused FIFO changed: %v", statErr)
				}
			}
			if mutation == "hardlink" {
				if got := readFileString(t, path); got != seeded {
					t.Fatalf("refused hardlink bytes changed: %s", got)
				}
				info, statErr := os.Lstat(path)
				if statErr != nil || !os.SameFile(info, seededInfo) || info.Mode() != seededInfo.Mode() {
					t.Fatalf("refused hardlink identity/mode changed: %v", statErr)
				}
				if info, statErr := os.Lstat(extraLink); statErr != nil || !os.SameFile(info, seededInfo) {
					t.Fatalf("extra hardlink changed: %v", statErr)
				}
			}
			if data, readErr := os.ReadFile(externalPath); mutation != "contained-alias" && (readErr != nil || string(data) != seeded) {
				t.Fatalf("external bytes changed: %q %v", data, readErr)
			}
			if info, statErr := os.Lstat(externalPath); statErr != nil || !os.SameFile(info, externalInfo) || info.Mode() != externalInfo.Mode() {
				t.Fatalf("external identity/mode changed: %v", statErr)
			}
		})
	}
}

// spec: materialization/preview-preserves-existing-owned-file-and-link-paths
func TestImpliedPreviewRetainsCommandDirOutputsAcrossTargets(t *testing.T) {
	nativeRel := func(t *testing.T, target hcc.Target, name string) string {
		t.Helper()
		rel, ok := targetmeta.NativeResourcePath(target, ir.KindCommand, name)
		if !ok {
			t.Fatalf("target %s declares no command resource path", target)
		}
		return rel
	}
	factoryRel := func(t *testing.T, _ hcc.Target, name string) string {
		t.Helper()
		info, ok := harness.TargetInfoFor(hcc.TargetFactory)
		if !ok {
			t.Fatal("factory target info missing")
		}
		dir := info.ResourceInstallDir(ir.KindCommand)
		if dir == "" {
			t.Fatal("factory declares no command install dir")
		}
		return dir + "/" + name + ".md"
	}
	for _, tc := range []struct {
		name    string
		target  hcc.Target
		machine bool
		root    func(repo, home string) string
		rel     func(t *testing.T, target hcc.Target, name string) string
	}{
		{name: "devin-repo", target: hcc.TargetDevin, root: func(repo, _ string) string { return devinProjectRoot(repo) }, rel: nativeRel},
		{name: "devin-machine", target: hcc.TargetDevin, machine: true, root: func(_, home string) string { return devinUserRoot(home) }, rel: nativeRel},
		{name: "codex-repo", target: hcc.TargetCodex, root: func(repo, _ string) string { return repo }, rel: nativeRel},
		{name: "codex-machine", target: hcc.TargetCodex, machine: true, root: func(_, home string) string { return home }, rel: nativeRel},
		{name: "factory-repo", target: hcc.TargetFactory, root: func(repo, _ string) string { return filepath.Join(repo, ".factory") }, rel: factoryRel},
		{name: "factory-machine", target: hcc.TargetFactory, machine: true, root: func(_, home string) string { return filepath.Join(home, ".factory") }, rel: factoryRel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, home := devinEnv(t)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			commandArtifact := func(name string, preview bool) resolver.ResolvedArtifact {
				art := cleanupLinkSkill(name, tc.machine, tc.target)
				art.Kind = ir.KindCommand
				art.Resource = ir.NewCommand(name, "command", name+" body", nil, ir.SourceRef{})
				if preview {
					art.RequiredBy = []resolver.ResourceRef{{Kind: ir.KindSkill, Name: "parent"}}
				}
				return art
			}
			keep := commandArtifact("keep", true)
			stale := commandArtifact("zzstale", false)
			report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{keep, stale})
			if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
				t.Fatalf("seed=%#v err=%v", report, err)
			}
			root := tc.root(repo, home)
			keepPath := filepath.Join(root, filepath.FromSlash(tc.rel(t, tc.target, "keep")))
			stalePath := filepath.Join(root, filepath.FromSlash(tc.rel(t, tc.target, "zzstale")))
			for _, path := range []string{keepPath, stalePath} {
				if _, err := os.Lstat(path); err != nil {
					t.Fatalf("positive install control missing at %s: %v", path, err)
				}
			}

			selected := cleanupLinkSkill("parent", tc.machine, tc.target)
			report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{keep})
			requireNoErrorDiags(t, report.Diags)
			if _, err := os.Lstat(keepPath); err != nil {
				t.Fatalf("preview command output pruned at %s: %v", keepPath, err)
			}
			if _, err := os.Lstat(stalePath); !os.IsNotExist(err) {
				t.Fatalf("unrelated stale command output retained at %s: %v", stalePath, err)
			}
		})
	}
}

// spec: materialization/preview-preserves-existing-owned-file-and-link-paths
func TestImpliedPreviewFileModePreservesBytesAndHonestReport(t *testing.T) {
	repo, _ := devinEnv(t)
	modes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	helper := previewSkill("helper", false)
	stale := cleanupLinkSkill("zzstale", false, hcc.TargetDevin)
	report, err := Run(context.Background(), repo, []resolver.ResolvedArtifact{helper, stale}, WithMaterializationModes(modes))
	if err != nil || report == nil || hasErrorDiagnostics(report.Diags) {
		t.Fatalf("seed=%#v err=%v", report, err)
	}
	file := filepath.Join(repo, ".devin", "skills", "helper", "SKILL.md")
	before := readFileString(t, file)
	beforeInfo, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	manifestBefore := readFileString(t, manifestPath)
	for _, want := range []string{"helper", "zzstale"} {
		if !strings.Contains(manifestBefore, want) {
			t.Fatalf("seed manifest missing %s: %s", want, manifestBefore)
		}
	}

	selected := cleanupLinkSkill("parent", false, hcc.TargetDevin)
	report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{helper}, WithMaterializationModes(modes), WithDryRun())
	requireNoErrorDiags(t, report.Diags)
	tr := report.PerTarget[hcc.TargetDevin]
	for _, path := range append(append([]string{}, tr.Written...), tr.Removed...) {
		if strings.Contains(path, "helper") {
			t.Fatalf("dry-run falsely reported %s for the retained preview output (written=%v removed=%v)", path, tr.Written, tr.Removed)
		}
	}
	if got := readFileString(t, file); got != before {
		t.Fatalf("dry-run preview rewrote the file-copy output:\nbefore %s\nafter  %s", before, got)
	}

	for run := 0; run < 2; run++ {
		report = runPreview(t, repo, []resolver.ResolvedArtifact{selected}, []resolver.ResolvedArtifact{helper, previewSkill("missing", false)}, WithMaterializationModes(modes))
		requireNoErrorDiags(t, report.Diags)
		if got := readFileString(t, file); got != before {
			t.Fatalf("mutating preview run %d rewrote the retained output:\nbefore %s\nafter  %s", run, before, got)
		}
		info, statErr := os.Lstat(file)
		if statErr != nil || info.Mode() != beforeInfo.Mode() || !info.ModTime().Equal(beforeInfo.ModTime()) {
			t.Fatalf("mutating preview run %d changed the retained file identity/mode/mtime: %v", run, statErr)
		}
		if _, statErr := os.Lstat(filepath.Join(repo, ".devin", "skills", "missing")); !os.IsNotExist(statErr) {
			t.Fatalf("mutating preview run %d created a missing output: %v", run, statErr)
		}
		manifest := readFileString(t, manifestPath)
		if !strings.Contains(manifest, "helper") {
			t.Fatalf("mutating preview run %d dropped the manifest proof: %s", run, manifest)
		}
		if strings.Contains(manifest, "zzstale") {
			t.Fatalf("mutating preview run %d kept the stale manifest entry: %s", run, manifest)
		}
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".devin", "skills", "zzstale")); !os.IsNotExist(statErr) {
		t.Fatalf("unrelated stale output survived preview retention: %v", statErr)
	}
}
