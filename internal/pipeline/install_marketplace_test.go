package pipeline

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestMarketplaceInstallPreviewUsesPinnedAcquisitionAndExactRegistry(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifestDigest := "sha256:" + strings.Repeat("b", 64)
	plan := &marketplace.PreparedInstall{
		Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		Pin: config.MarketplaceResourcePin{
			RegistryID: "source-id", RegistrySlug: "source", Kind: "skill", Name: "review",
			Revision: "sha256:" + strings.Repeat("c", 64), Digest: "sha256:" + strings.Repeat("d", 64),
			Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: commit, RegistryManifestDigest: manifestDigest,
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
	if typed.Acquisition != config.MarketplaceAcquisitionIndexOnly || typed.RegistryCommit != commit || typed.RegistryManifestDigest != manifestDigest {
		t.Fatalf("typed preview lost exact index-only acquisition: %#v", typed)
	}
	for _, want := range []string{
		`acquisition: "index-only"`, `registry_commit: "` + commit + `"`,
		`registry_manifest_digest: "` + manifestDigest + `"`,
	} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
	if strings.Contains(preview, "acquisition: mirror") {
		t.Fatalf("preview hardcoded mirror:\n%s", preview)
	}
}

func TestMarketplaceCollectionInstallPreviewIncludesRootAndSortedLeafMetadata(t *testing.T) {
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "collection-source", Kind: "collection", Name: "secure"},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
	}
	root := &marketplace.PreparedInstall{
		Entry:              config.MarketplaceEntry{Alias: "public", URL: "https://market.example", InstanceID: "mp"},
		Pin:                config.MarketplaceResourcePin{RegistryID: "collection-source", RegistrySlug: "collections", Kind: "collection", Name: "secure", Revision: "root-revision", Digest: "root-digest", Acquisition: config.MarketplaceAcquisitionMirror},
		Revision:           testAssessedResourceRevision(t, revision, store.AssessmentDecisionWarn, []store.Finding{{Code: "collection-policy", Severity: "warning", Policy: "warn", Message: "review collection"}}),
		CollectionManifest: hccartifact.CollectionManifest{Provenance: hccartifact.CanonicalProvenance{RegistryURL: "https://market.example", Ref: "main", Path: "collections/secure", Generator: "test"}},
		CollectionMembers: []*marketplace.PreparedInstall{
			{
				Pin: config.MarketplaceResourcePin{Kind: "collection", Name: "base", Revision: "base-revision", Digest: "base-digest", Acquisition: config.MarketplaceAcquisitionMirror},
				CollectionMembers: []*marketplace.PreparedInstall{{
					Pin:      config.MarketplaceResourcePin{RegistryID: "z-source", RegistrySlug: "z", Kind: "skill", Name: "zeta", Revision: "z-revision", Digest: "z-digest", Acquisition: config.MarketplaceAcquisitionMirror},
					Artifact: hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{Capabilities: []string{"read", "write"}}},
				}},
			},
			{
				Pin:      config.MarketplaceResourcePin{RegistryID: "a-source", RegistrySlug: "a", Kind: "command", Name: "alpha", Revision: "a-revision", Digest: "a-digest", Acquisition: config.MarketplaceAcquisitionIndexOnly, RegistryCommit: strings.Repeat("c", 40), RegistryManifestDigest: "sha256:" + strings.Repeat("d", 64)},
				Artifact: hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{Capabilities: []string{"execute"}}},
			},
		},
	}
	preview, err := marketplaceInstallPreview(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`kind: "collection"`, `name: "secure"`, `collection-policy`,
		`"kind":"command"`, `"name":"alpha"`, `"revision":"a-revision"`, `"acquisition":"index-only"`,
		`"registry_commit":"` + strings.Repeat("c", 40) + `"`, `"registry_manifest_digest":"sha256:` + strings.Repeat("d", 64) + `"`,
		`"kind":"skill"`, `"name":"zeta"`, `"acquisition":"mirror"`, `"capabilities":["execute"]`,
	} {
		if !strings.Contains(preview, want) {
			t.Fatalf("collection preview missing %q:\n%s", want, preview)
		}
	}
	if strings.Index(preview, `"name":"alpha"`) > strings.Index(preview, `"name":"zeta"`) {
		t.Fatalf("collection leaves are not deterministic:\n%s", preview)
	}
	typed := root.Preview()
	if len(typed.CollectionMembers) != 2 || len(typed.Findings) != 1 || typed.Findings[0].Code != "collection-policy" {
		t.Fatalf("typed collection preview = %#v", typed)
	}
}

func TestMarketplaceCollectionConsentEvaluatesRevisionBeforeLeaves(t *testing.T) {
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "collection-source", Kind: "collection", Name: "secure"},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
	}
	root := &marketplace.PreparedInstall{
		Pin:               config.MarketplaceResourcePin{Kind: "collection", Name: "secure"},
		Revision:          testAssessedResourceRevision(t, revision, store.AssessmentDecisionReject, []store.Finding{{Code: "collection-block", Severity: "high", Policy: "reject", Message: "blocked collection"}}),
		CollectionMembers: []*marketplace.PreparedInstall{{Artifact: hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{Findings: []hccartifact.CanonicalFinding{{Code: "leaf-block", PolicyOutcome: "reject"}}}}}},
	}
	if err := evaluateMarketplaceConsent(root, true); err == nil || !strings.Contains(err.Error(), "collection-block") {
		t.Fatalf("collection consent error = %v, want collection revision finding", err)
	}
}

func TestMarketplaceLocatorWithSelectorSupportsAllNonCollectionKinds(t *testing.T) {
	for _, kind := range []string{"skill", "command", "subagent", "hook", "mcpserver"} {
		t.Run(kind, func(t *testing.T) {
			loc, err := marketplaceLocator("source/"+kind+"/name", "public")
			if err != nil {
				t.Fatalf("marketplaceLocator: %v", err)
			}
			if loc.Alias != "public" || loc.Registry != "source" || loc.Kind != kind || loc.Name != "name" {
				t.Fatalf("locator = %#v", loc)
			}
		})
	}
}

func TestMarketplaceLocatorSupportsBrowserAndAdHocEndpoint(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	loc, err := marketplaceLocator("https://market.example/r/registry/skill/review/revisions/"+digest, "")
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
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
	}
	plan := &marketplace.PreparedInstall{
		Revision: testAssessedResourceRevision(t, revision, store.AssessmentDecisionWarn, []store.Finding{{Code: "exec", Severity: "high", Policy: "warn"}}),
		Artifact: hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{Capabilities: []string{"command:exec"}}},
	}
	if err := evaluateMarketplaceConsent(plan, false); err == nil || !strings.Contains(err.Error(), "risk acceptance") {
		t.Fatalf("consent error = %v", err)
	}
	if err := evaluateMarketplaceConsent(plan, true); err != nil {
		t.Fatalf("accepted risk rejected: %v", err)
	}
	plan.Revision = testAssessedResourceRevision(t, revision, store.AssessmentDecisionReject, []store.Finding{{Code: "exec", Severity: "high", Policy: "reject"}})
	if err := evaluateMarketplaceConsent(plan, true); err == nil || !strings.Contains(err.Error(), "cannot bypass") {
		t.Fatalf("policy rejection error = %v", err)
	}
	plan.Revision = testAssessedResourceRevision(t, revision, store.AssessmentDecisionReject, []store.Finding{{Code: "exec", Severity: "high", Policy: "block"}})
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

func TestMarketplaceCollisionWorksWithoutConfiguredRegistries(t *testing.T) {
	plan := &marketplace.PreparedInstall{
		Pin:      config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review"},
		Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}},
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
	}
	if err := checkMarketplaceInstallCollision(context.Background(), InstallOptions{RepoRoot: t.TempDir()}, &config.EffectiveConfig{Merged: &config.Config{}}, plan); err != nil {
		t.Fatalf("no-registry collision check: %v", err)
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: "https://other.example", InstanceID: "other", Resources: []config.MarketplaceResourcePin{{RegistryID: "other", Kind: "skill", Name: "review"}}}}}
	if err := checkMarketplaceInstallCollision(context.Background(), InstallOptions{RepoRoot: t.TempDir()}, &config.EffectiveConfig{Merged: cfg}, plan); err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestMarketplaceCollisionsCannotBeBypassedWithInstalledName(t *testing.T) {
	plan := &marketplace.PreparedInstall{
		Pin:      config.MarketplaceResourcePin{RegistryID: "new-source", Kind: "skill", Name: "review", InstalledName: "review-copy"},
		Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}},
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "new-marketplace"},
	}
	marketplaceConfig := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "HTTPS://OTHER.example/", InstanceID: "other-marketplace",
		Resources: []config.MarketplaceResourcePin{{RegistryID: "other-source", Kind: "skill", Name: "other", InstalledName: "review-copy"}},
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
		Pin:      config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", InstalledName: "review-copy"},
		Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}},
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
	}
	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{
		URL: "HTTPS://MARKET.example/", InstanceID: "mp",
		Resources: []config.MarketplaceResourcePin{{RegistryID: "src", Kind: "skill", Name: "review", InstalledName: "review-copy"}},
	}}}
	if err := checkMarketplaceInstallCollision(context.Background(), InstallOptions{RepoRoot: t.TempDir()}, &config.EffectiveConfig{Merged: cfg}, plan); err != nil {
		t.Fatalf("same canonical Marketplace pin collided: %v", err)
	}
}

func TestMarketplaceCollectionLeavesRejectMaterializedNameCollision(t *testing.T) {
	leaf := func(source, name string) *marketplace.PreparedInstall {
		return &marketplace.PreparedInstall{
			Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
			Pin:      config.MarketplaceResourcePin{RegistryID: source, Kind: "skill", Name: name, InstalledName: "same"},
			Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: name}},
		}
	}
	plan := &marketplace.PreparedInstall{
		Entry:             config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		CollectionMembers: []*marketplace.PreparedInstall{leaf("src-a", "first"), leaf("src-b", "second")},
	}
	if err := checkMarketplaceInstallCollision(context.Background(), InstallOptions{RepoRoot: t.TempDir()}, &config.EffectiveConfig{Merged: &config.Config{}}, plan); err == nil || !strings.Contains(err.Error(), "collection leaves") {
		t.Fatalf("collection leaf collision error = %v, want pre-mutation collision", err)
	}
}

func TestMarketplaceCollectionLeavesAllowSameExactMarketplaceIdentity(t *testing.T) {
	leaf := &marketplace.PreparedInstall{
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		Pin:      config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", InstalledName: "same"},
		Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}},
	}
	plan := &marketplace.PreparedInstall{
		Entry:             config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		CollectionMembers: []*marketplace.PreparedInstall{leaf, {Entry: leaf.Entry, Pin: leaf.Pin, Resolved: leaf.Resolved}},
	}
	if err := checkMarketplaceInstallCollision(context.Background(), InstallOptions{RepoRoot: t.TempDir()}, &config.EffectiveConfig{Merged: &config.Config{}}, plan); err != nil {
		t.Fatalf("same exact collection leaf identity collided: %v", err)
	}
}

func TestCommitPreparedMarketplaceInstallRejectsPolicyChangeWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		name       string
		policyYAML string
		want       string
	}{
		{name: "marketplace allowlist", policyYAML: "allowed_marketplaces:\n  - 127\\.0\\.0\\.1\n", want: "allowed_marketplaces"},
		{name: "registry nil to empty", policyYAML: "allowed_registries: []\n", want: "allowed_registries"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
			t.Setenv("HOME", t.TempDir())
			repoRoot := t.TempDir()
			configPath := config.ProjectConfigPath(repoRoot)
			if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
				t.Fatal(err)
			}
			before := []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n" + test.policyYAML + "marketplaces:\n  - url: http://127.0.0.1:43210\n    instance_id: marketplace-id\n")
			if err := os.WriteFile(configPath, before, 0o644); err != nil {
				t.Fatal(err)
			}
			prepared := &PreparedMarketplaceInstall{
				options:            InstallOptions{RepoRoot: repoRoot},
				configPath:         configPath,
				plan:               &marketplace.PreparedInstall{Entry: config.MarketplaceEntry{URL: "http://127.0.0.1:43210", InstanceID: "marketplace-id"}},
				targetHarnesses:    []string{"factory"},
				endpointConfigured: true,
			}

			_, err := CommitPreparedMarketplaceInstall(context.Background(), prepared)
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "preview again") {
				t.Fatalf("error = %v, want actionable %s policy change", err, test.want)
			}
			after, readErr := os.ReadFile(configPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("config changed after policy refusal:\n%s", after)
			}
		})
	}
}

func TestMarketplaceInstallRequiresTargetHarnessSetBeforeMutation(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	_, err := prepareMarketplaceInstall(context.Background(), InstallOptions{Args: []string{"public:source/skill/review"}}, configPath, &config.EffectiveConfig{})
	var missing *TargetHarnessSetMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v, want TargetHarnessSetMissingError", err)
	}
	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("config mutation occurred: stat error = %v", statErr)
	}
}

func TestMarketplaceInstallPreviewFormatsFindingsForHumans(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	revision := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
	}
	plan := &marketplace.PreparedInstall{
		Pin:      config.MarketplaceResourcePin{RegistryID: "src", RegistrySlug: "slug", Kind: "skill", Name: "review", Revision: digest, Digest: digest},
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		Revision: testAssessedResourceRevision(t, revision, store.AssessmentDecisionWarn, []store.Finding{{Code: "secret", Severity: "high", Message: "line 1\nsecret=do-not-log\x1b[31m\u202e", Location: &store.FindingLocation{Path: "skills/review/SKILL.md\tunsafe", Line: 12}, Policy: "warn"}}, "scanner\r"),
		Artifact: hccartifact.CanonicalArtifact{Manifest: hccartifact.CanonicalManifest{Capabilities: []string{"exec\x1b\n"}, Provenance: hccartifact.CanonicalProvenance{RegistryURL: "https://market.example\nunsafe", Ref: "main\u202e", Path: "skills/review", Generator: "scanner\x1b"}}},
	}
	preview, err := marketplaceInstallPreview(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"findings (1):", `  1. severity: "high"`, `     message: "line 1\nsecret=do-not-log`,
		`     location: "skills/review/SKILL.md\tunsafe":12`, `     code: "secret"`,
		`     policy: "warn"`, `     analyzer: "scanner\r"`,
	} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q: %s", want, preview)
		}
	}
	if strings.Contains(preview, `findings: [{"code"`) {
		t.Fatalf("preview renders findings as JSON: %s", preview)
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

func TestMarketplaceCollisionUsesExactLockView(t *testing.T) {
	for _, tc := range []struct {
		name          string
		selected      string
		defaultSelect bool
		wantCollision bool
	}{
		{name: "locked selection absent from mutable cache", selected: "keeper", wantCollision: true},
		{name: "mutable-only selection absent from lock", selected: "cache-only", defaultSelect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			ec, _ := exactLockViewGCFixture(t)
			repoRoot := filepath.Dir(filepath.Dir(ec.Repo.Path))
			entry := ec.Merged.Registries[0]
			selection := "    skills: {include: [keeper]}\n"
			if tc.defaultSelect {
				selection = "    default: true\n"
			}
			configBytes := []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: " + entry.URL + "\n    ref: " + entry.Ref + "\n" + selection)
			if err := os.WriteFile(ec.Repo.Path, configBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
			lockBytes, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			warm := cache.RegistryCachePath(entry.URL, entry.Ref)
			if err := os.MkdirAll(filepath.Join(warm, ".factory", "skills", "cache-only"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(warm, ".factory", "skills", "cache-only", "SKILL.md"), []byte("---\nname: cache-only\ndescription: cache\n---\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			loaded := config.LoadEffective(context.Background(), config.LoadEffectiveOptions{RepoRoot: repoRoot})
			if len(loaded.Errors) > 0 {
				t.Fatal(errors.Join(loaded.Errors...))
			}
			var clones atomic.Int32
			opts := InstallOptions{RepoRoot: repoRoot, CatalogOption: []catalog.LoadOption{catalog.WithCloneOrPull(func(string, string) (string, error) {
				clones.Add(1)
				return "", errors.New("unexpected clone")
			})}}
			plan := &marketplace.PreparedInstall{Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}, Pin: config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: tc.selected}, Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: tc.selected}}}
			err = checkMarketplaceInstallCollision(context.Background(), opts, &loaded, plan)
			if tc.wantCollision != (err != nil) {
				t.Fatalf("collision error = %v, want collision %v", err, tc.wantCollision)
			}
			if tc.wantCollision && !strings.Contains(err.Error(), `skill "keeper" collides with installed name "keeper"`) {
				t.Fatalf("collision error = %v, want keeper collision", err)
			}
			if clones.Load() != 0 {
				t.Fatalf("clone calls = %d", clones.Load())
			}
			gotConfig, configErr := os.ReadFile(ec.Repo.Path)
			if configErr != nil {
				t.Fatal(configErr)
			}
			gotLock, lockErr := os.ReadFile(lockPath)
			if lockErr != nil {
				t.Fatal(lockErr)
			}
			if !bytes.Equal(gotConfig, configBytes) || !bytes.Equal(gotLock, lockBytes) {
				t.Fatal("collision check mutated config or lock")
			}
			if _, err := os.Stat(filepath.Join(repoRoot, ".factory")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("collision check created harness output: %v", err)
			}
		})
	}
}

func TestMarketplaceCollisionRejectsInvalidOrUnavailableExactLock(t *testing.T) {
	for _, tc := range []struct {
		name         string
		code         string
		breakFixture func(t *testing.T, repoRoot, snapshot string)
	}{
		{name: "invalid lock", code: "lock.invalid", breakFixture: func(t *testing.T, repoRoot, _ string) {
			if err := os.WriteFile(filepath.Join(repoRoot, ".agents", "lock.yaml"), []byte("not: [valid"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unavailable snapshot", code: "lock.snapshot-unavailable", breakFixture: func(t *testing.T, _ string, snapshot string) {
			if err := os.RemoveAll(snapshot); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			ec, snapshot := exactLockViewGCFixture(t)
			repoRoot := filepath.Dir(filepath.Dir(ec.Repo.Path))
			tc.breakFixture(t, repoRoot, snapshot)
			loaded := config.LoadEffective(context.Background(), config.LoadEffectiveOptions{RepoRoot: repoRoot})
			var clones atomic.Int32
			opts := InstallOptions{RepoRoot: repoRoot, CatalogOption: []catalog.LoadOption{catalog.WithCloneOrPull(func(string, string) (string, error) {
				clones.Add(1)
				return "", errors.New("unexpected clone")
			})}}
			plan := &marketplace.PreparedInstall{Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"}, Pin: config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "keeper"}, Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "keeper"}}}
			err := checkMarketplaceInstallCollision(context.Background(), opts, &loaded, plan)
			var lockErr *LockViewError
			if !errors.As(err, &lockErr) || lockErr.Code != tc.code {
				t.Fatalf("error = %v, want LockViewError %s", err, tc.code)
			}
			if clones.Load() != 0 {
				t.Fatalf("clone calls = %d", clones.Load())
			}
		})
	}
}

func TestMarketplaceInstallDoesNotRefreshWarmRegistries(t *testing.T) {
	opts, ec, plan, fetches := marketplaceInstallWarmRegistryFixture(t, 0)
	configPath := config.ProjectConfigPath(opts.RepoRoot)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkMarketplaceInstallCollision(context.Background(), opts, ec, plan); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("missing-lock preflight changed config: %q err=%v", after, err)
	}
	if _, err := os.Stat(filepath.Join(opts.RepoRoot, ".factory")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-lock preflight created harness output: %v", err)
	}
	if _, err := syncAfterInstall(context.Background(), opts.RepoRoot, nil, silentSyncOptsForInstall(opts)...); err != nil {
		t.Fatal(err)
	}
	if got := fetches.Load(); got != 0 {
		t.Fatalf("warm registry refreshes = %d, want 0", got)
	}
	result := Sync(context.Background(), ResolveOptions{RepoRoot: opts.RepoRoot, CatalogOption: opts.CatalogOption})
	if err := errors.Join(result.Errors...); err != nil {
		t.Fatal(err)
	}
	if got := fetches.Load(); got != 1 {
		t.Fatalf("warm registry refreshes after explicit sync = %d, want 1", got)
	}
}

func BenchmarkMarketplaceInstallWarmRegistries(b *testing.B) {
	opts, ec, plan, fetches := marketplaceInstallWarmRegistryFixture(b, time.Millisecond)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := checkMarketplaceInstallCollision(context.Background(), opts, ec, plan); err != nil {
			b.Fatal(err)
		}
		if _, err := syncAfterInstall(context.Background(), opts.RepoRoot, nil, silentSyncOptsForInstall(opts)...); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(fetches.Load())/float64(b.N), "refreshes/op")
}

func marketplaceInstallWarmRegistryFixture(tb testing.TB, fetchDelay time.Duration) (InstallOptions, *config.EffectiveConfig, *marketplace.PreparedInstall, *atomic.Int32) {
	tb.Helper()
	home := tb.TempDir()
	repo := tb.TempDir()
	tb.Setenv("HOME", home)
	config.SetTestOrganizationConfigPath(tb, filepath.Join(home, "missing-org.yaml"))
	registryURL := "https://example.com/registry.git"
	registryCache := cache.RegistryCachePath(registryURL, "main")
	if err := os.MkdirAll(registryCache, 0o755); err != nil {
		tb.Fatal(err)
	}
	configPath := config.ProjectConfigPath(repo)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: "+registryURL+"\n    ref: main\n"), 0o644); err != nil {
		tb.Fatal(err)
	}
	var fetches atomic.Int32
	opts := InstallOptions{
		RepoRoot: repo,
		CatalogOption: []catalog.LoadOption{
			catalog.WithCloneOrPull(func(url, _ string) (string, error) {
				if url == registryURL {
					fetches.Add(1)
					time.Sleep(fetchDelay)
				}
				return registryCache, nil
			}),
			catalog.WithDiscoverFunc(func(string, string) (catalog.LoadedCatalog, error) { return catalog.LoadedCatalog{}, nil }),
		},
	}
	cfg := &config.Config{APIVersion: config.APIVersionV2, Harnesses: []string{"factory"}, Registries: []config.RegistryEntry{{URL: registryURL, Ref: "main"}}}
	plan := &marketplace.PreparedInstall{
		Entry:    config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp"},
		Pin:      config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review"},
		Resolved: resolver.ResolvedArtifact{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "review"}},
	}
	return opts, &config.EffectiveConfig{Merged: cfg}, plan, &fetches
}

func testMarketplacePreparedInstall() *marketplace.PreparedInstall {
	digest := "sha256:" + strings.Repeat("a", 64)
	return &marketplace.PreparedInstall{
		Entry: config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Alias: "public"},
		Pin:   config.MarketplaceResourcePin{RegistryID: "src", RegistrySlug: "source", Kind: "skill", Name: "review", Revision: digest, Digest: digest},
	}
}

func TestMarketplaceDirectInstallPersistsDirectSelection(t *testing.T) {
	pins := marketplaceInstallPins(testMarketplacePreparedInstall())
	if len(pins) != 1 || !pins[0].Direct {
		t.Fatalf("direct install pins = %#v, want direct marker", pins)
	}
}
