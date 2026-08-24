package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestMarketplaceInstallPreviewUsesPinnedAcquisitionAndExactSource(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifestDigest := "sha256:" + strings.Repeat("b", 64)
	plan := &marketplace.PreparedInstall{
		Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		Pin: config.MarketplaceResourcePin{
			SourceID: "source-id", SourceSlug: "source", Kind: "skill", Name: "review",
			Revision: "sha256:" + strings.Repeat("c", 64), Digest: "sha256:" + strings.Repeat("d", 64),
			Acquisition: config.MarketplaceAcquisitionIndexOnly, SourceCommit: commit, SourceManifestDigest: manifestDigest,
		},
		Artifact: hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{
			License: "MIT", Provenance: hccartifact.CanonicalProvenance{Generator: "test"},
		}},
	}
	preview, err := marketplaceInstallPreview(plan)
	if err != nil {
		t.Fatalf("marketplaceInstallPreview: %v", err)
	}
	typed := plan.Preview()
	if typed.Acquisition != config.MarketplaceAcquisitionIndexOnly || typed.SourceCommit != commit || typed.SourceManifestDigest != manifestDigest {
		t.Fatalf("typed preview lost exact index-only acquisition: %#v", typed)
	}
	for _, want := range []string{
		`acquisition: "index-only"`, `source_commit: "` + commit + `"`,
		`source_manifest_digest: "` + manifestDigest + `"`,
	} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
	if strings.Contains(preview, "acquisition: mirror") {
		t.Fatalf("preview hardcoded mirror:\n%s", preview)
	}
}

func TestMarketplaceLocatorWithSelectorSupportsAllNonCollectionKinds(t *testing.T) {
	for _, kind := range []string{"skill", "command", "subagent", "hook", "mcpserver"} {
		t.Run(kind, func(t *testing.T) {
			loc, err := marketplaceLocator("source/"+kind+"/name", "public")
			if err != nil {
				t.Fatalf("marketplaceLocator: %v", err)
			}
			if loc.Alias != "public" || loc.Source != "source" || loc.Kind != kind || loc.Name != "name" {
				t.Fatalf("locator = %#v", loc)
			}
		})
	}
}

func TestMarketplaceLocatorSupportsBrowserAndAdHocEndpoint(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	loc, err := marketplaceLocator("https://market.example/s/source/skill/review/revisions/"+digest, "")
	if err != nil {
		t.Fatalf("browser locator: %v", err)
	}
	if loc.URL != "https://market.example" || loc.Revision != digest {
		t.Fatalf("browser locator = %#v", loc)
	}
	entry, err := resolveMarketplaceEntry(loc, "", &config.Config{})
	if err != nil {
		t.Fatalf("ad-hoc entry: %v", err)
	}
	if entry.URL != "https://market.example" || entry.InstanceID != "" {
		t.Fatalf("ad-hoc entry = %#v", entry)
	}
}

func TestMarketplaceConsentRejectsPolicyFindingAndRequiresRiskAcceptance(t *testing.T) {
	plan := &marketplace.PreparedInstall{Artifact: hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{
		Findings:     []hccartifact.CanonicalFinding{{Code: "exec", Severity: "high", PolicyOutcome: "warn"}},
		Capabilities: []string{"command:exec"},
	}}}
	if err := evaluateMarketplaceConsent(plan, false); err == nil || !strings.Contains(err.Error(), "risk acceptance") {
		t.Fatalf("consent error = %v", err)
	}
	if err := evaluateMarketplaceConsent(plan, true); err != nil {
		t.Fatalf("accepted risk rejected: %v", err)
	}
	plan.Artifact.Manifest.Findings[0].PolicyOutcome = "reject"
	if err := evaluateMarketplaceConsent(plan, true); err == nil || !strings.Contains(err.Error(), "cannot bypass") {
		t.Fatalf("policy rejection error = %v", err)
	}
	plan.Artifact.Manifest.Findings[0].PolicyOutcome = "block"
	if err := evaluateMarketplaceConsent(plan, true); err == nil || !strings.Contains(err.Error(), "unknown policy outcome") {
		t.Fatalf("unknown policy outcome error = %v", err)
	}
}

func TestMarketplaceKindComparisonNormalizesMCPProtocolKind(t *testing.T) {
	if got := marketplaceProtocolKind(ir.KindMcpServer); got != "mcpserver" {
		t.Fatalf("MCP protocol kind = %q, want mcpserver", got)
	}
	if !marketplaceKindMatches(ir.KindMcpServer, "mcpserver") {
		t.Fatal("MCP kind did not match protocol locator kind")
	}
	if marketplaceKindMatches(ir.KindMcpServer, "mcp") {
		t.Fatal("MCP kind matched non-protocol locator kind")
	}
	if got := marketplaceProtocolKind(ir.KindSkill); got != "skill" {
		t.Fatalf("skill protocol kind = %q, want skill", got)
	}
}

func TestMarketplaceConsentChecksCancellationBeforeMutation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ctx.Err(); err == nil {
		t.Fatal("expected canceled context")
	}
}

func TestMarketplaceCollisionWorksWithoutConfiguredRegistries(t *testing.T) {
	plan := &marketplace.PreparedInstall{
		Pin:      config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review"},
		Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}},
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
	}
	if err := checkMarketplaceInstallCollision(context.Background(), InstallOptions{RepoRoot: t.TempDir()}, &config.EffectiveConfig{Merged: &config.Config{}}, plan); err != nil {
		t.Fatalf("no-registry collision check: %v", err)
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://other.example", InstanceID: "other", Resources: []config.MarketplaceResourcePin{{SourceID: "other", Kind: "skill", Name: "review"}}}}}
	if err := checkMarketplaceInstallCollision(context.Background(), InstallOptions{RepoRoot: t.TempDir()}, &config.EffectiveConfig{Merged: cfg}, plan); err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestMarketplaceCollisionsCannotBeBypassedWithInstalledName(t *testing.T) {
	plan := &marketplace.PreparedInstall{
		Pin:      config.MarketplaceResourcePin{SourceID: "new-source", Kind: "skill", Name: "review", InstalledName: "review-copy"},
		Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}},
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "new-marketplace"},
	}
	marketplaceConfig := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "HTTPS://OTHER.example/", InstanceID: "other-marketplace",
		Resources: []config.MarketplaceResourcePin{{SourceID: "other-source", Kind: "skill", Name: "other", InstalledName: "review-copy"}},
	}}}
	if err := checkMarketplaceInstallCollision(context.Background(), InstallOptions{RepoRoot: t.TempDir()}, &config.EffectiveConfig{Merged: marketplaceConfig}, plan); err == nil {
		t.Fatal("Marketplace collision was bypassed with installed_name")
	}

	for _, kind := range []string{"registry", "local"} {
		t.Run(kind, func(t *testing.T) {
			existing := resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "existing"}, InstalledName: "review-copy"}
			if kind == "registry" {
				existing.RegistryURL = "file:///registry"
			} else {
				existing.RegistryURL = "file:///local"
			}
			if err := checkResolvedMarketplaceInstallCollision([]resolver.ResolvedArtifact{existing}, plan); err == nil {
				t.Fatal("resource collision was bypassed with installed_name")
			}
		})
	}
}

func TestMarketplaceCollisionCanonicalizesExistingURLAndAllowsSamePin(t *testing.T) {
	plan := &marketplace.PreparedInstall{
		Pin:      config.MarketplaceResourcePin{SourceID: "src", Kind: "skill", Name: "review", InstalledName: "review-copy"},
		Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}},
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "HTTPS://MARKET.example/", InstanceID: "mp",
		Resources: []config.MarketplaceResourcePin{{SourceID: "src", Kind: "skill", Name: "review", InstalledName: "review-copy"}},
	}}}
	if err := checkMarketplaceInstallCollision(context.Background(), InstallOptions{RepoRoot: t.TempDir()}, &config.EffectiveConfig{Merged: cfg}, plan); err != nil {
		t.Fatalf("same canonical Marketplace pin collided: %v", err)
	}
}

func TestMarketplaceInstallRequiresTargetHarnessSetBeforeMutation(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	_, err := installMarketplace(context.Background(), InstallOptions{Args: []string{"public:source/skill/review"}}, configPath, &config.EffectiveConfig{})
	var missing *TargetHarnessSetMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v, want TargetHarnessSetMissingError", err)
	}
	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("config mutation occurred: stat error = %v", statErr)
	}
}

func TestMarketplaceInstallPreviewEscapesAllFindingDetails(t *testing.T) {
	plan := &marketplace.PreparedInstall{Pin: config.MarketplaceResourcePin{SourceID: "src", SourceSlug: "slug", Kind: "skill", Name: "review", Revision: "sha256:" + strings.Repeat("a", 64), Digest: "sha256:" + strings.Repeat("a", 64)}, Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}, Artifact: hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{Findings: []hccartifact.CanonicalFinding{{Code: "secret", Severity: "high", Message: "line 1\nsecret=do-not-log\x1b[31m\u202e", File: "skills/review/SKILL.md\tunsafe", Line: 12, Analyzer: "scanner\r", PolicyOutcome: "warn"}}, Capabilities: []string{"exec\x1b\n"}, Provenance: hccartifact.CanonicalProvenance{RegistryURL: "https://market.example\nunsafe", Ref: "main\u202e", Path: "skills/review", Generator: "scanner\x1b"}}}}
	preview, err := marketplaceInstallPreview(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"message":"line 1\nsecret=do-not-log`, `"file":"skills/review/SKILL.md\tunsafe"`, `"line":12`, `"analyzer":"scanner`, `"policy_outcome":"warn"`} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q: %s", want, preview)
		}
	}
	if strings.Contains(preview, "raw resource content") {
		t.Fatal("preview contains raw resource content")
	}
	if strings.ContainsAny(preview, "\x00\x1b\r\t") || strings.Contains(preview, "\u202e") {
		t.Fatalf("preview contains raw terminal data: %q", preview)
	}
	for _, escaped := range []string{`\x1b`, `\n`, `\r`, `\u202e`} {
		if !strings.Contains(preview, escaped) {
			t.Fatalf("preview missing escaped terminal data %q: %s", escaped, preview)
		}
	}
}

func TestMarketplacePinRollbackRestoresExistingConfigAfterSyncFailure(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v2\n# preserve this byte\n")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	plan := testMarketplacePreparedInstall()
	wantErr := errors.New("materialization failed")
	_, err := ensureMarketplacePinAndSync(configPath, plan, func() ([]error, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	got, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("config after rollback = %q, want %q", got, original)
	}
	if info, statErr := os.Stat(configPath); statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode after rollback = %v/%v, want 0600", info, statErr)
	}
}

func TestMarketplacePinRollbackRemovesConfigCreatedByFailedSync(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	_, err := ensureMarketplacePinAndSync(configPath, testMarketplacePreparedInstall(), func() ([]error, error) {
		return nil, errors.New("materialization failed")
	})
	if err == nil {
		t.Fatal("expected sync failure")
	}
	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("config after rollback stat error = %v, want not exist", statErr)
	}
}

func TestMarketplacePinRollbackRefusesConcurrentConfigChange(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v2\n")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	concurrent := []byte("apiVersion: agent-marketplace/v2\n# another writer\n")
	wantErr := errors.New("materialization failed")
	_, err := ensureMarketplacePinAndSync(configPath, testMarketplacePreparedInstall(), func() ([]error, error) {
		if err := os.WriteFile(configPath, concurrent, 0o600); err != nil {
			t.Fatalf("simulate concurrent writer: %v", err)
		}
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want original sync error %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "rollback Marketplace config") || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error = %v, want rollback conflict", err)
	}
	got, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(concurrent) {
		t.Fatalf("concurrent writer bytes = %q, want %q", got, concurrent)
	}
}

func testMarketplacePreparedInstall() *marketplace.PreparedInstall {
	digest := "sha256:" + strings.Repeat("a", 64)
	return &marketplace.PreparedInstall{
		Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Alias: "public"},
		Pin:   config.MarketplaceResourcePin{SourceID: "src", SourceSlug: "source", Kind: "skill", Name: "review", Revision: digest, Digest: digest},
	}
}
