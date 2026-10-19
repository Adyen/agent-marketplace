package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: marketplace-api/mixed-hosted-and-git-inventory-remains-complete
func TestResolveEmitsInventoryAtSharedBoundary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	exporter := newPipelineSpanExporter(t)
	ctx, tel := setupPipelineTelemetry(t, exporter)
	registryRoot := t.TempDir()
	writeSkill(t, registryRoot, "shared-inventory", "test", nil)
	marketplace := newLayerMarketplaceServer(t)
	hostedRegistry := marketplace.addRegistry(t, "awesome-ai")
	marketplace.addSkill(t, hostedRegistry, "canonical-review")
	hostedResource := marketplace.resources["awesome-ai/skill/canonical-review"]
	resolvedRevision := string(hostedResource.revision.ContentDigest)
	repoRoot := t.TempDir()
	repoPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	writeFile(t, repoPath, fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  registry: {directory: %q, source_harness: claude}
  hosted: {marketplace: %q}
resources:
  registry: [skill/shared-inventory]
  hosted:
    - awesome-ai/skill/canonical-review: {as: local-alias, revision: %q}
`, registryRoot, marketplace.URL, resolvedRevision))
	lockData, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      lockfile.ScopeRepo,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL: marketplace.URL, InstanceID: marketplace.instanceID,
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{{Name: hostedRegistry.Slug, RegistryID: string(hostedRegistry.ID)}},
			Resources: []lockfile.MarketplaceResourceLock{{
				RegistryName: hostedRegistry.Slug, RegistryID: string(hostedRegistry.ID), Kind: "skill", Name: "canonical-review",
				Revision: resolvedRevision, ContentDigest: lockfile.Digest(resolvedRevision), ArchiveDigest: lockfile.Digest(hostedResource.archiveDigest),
				Acquisition: lockfile.MarketplaceAcquisitionMirror,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("encode repository lock: %v", err)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	writeFile(t, lockPath, string(lockData))
	lockBefore, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	marketplaceMode := resolver.MarketplaceResolveModeRefresh
	result := Resolve(ctx, ResolveOptions{
		Workspace:       mustWorkspace(t, repoRoot),
		CatalogMode:     catalog.LoadModeInspect,
		MarketplaceMode: &marketplaceMode,
		ConfigLoadMode:  ConfigLoadStrict,
		EffectiveOpts: config.LoadEffectiveOptions{
			OrganizationPath: filepath.Join(t.TempDir(), "missing-org.yaml"),
		},
	})
	if len(result.Errors) != 0 {
		t.Fatalf("Resolve errors: %v", result.Errors)
	}
	if len(result.Resolved) != 2 {
		t.Fatalf("representative non-TUI Resolve result = %#v", result.Resolved)
	}
	var hostedFound bool
	for _, artifact := range result.Resolved {
		if artifact.IsMarketplace {
			hostedFound = artifact.Name == "local-alias" && artifact.MarketplaceResourceName == "canonical-review" &&
				artifact.MarketplaceInstanceID == marketplace.instanceID && artifact.MarketplaceRegistryID == string(hostedRegistry.ID) &&
				artifact.Source == config.RepoCommitted
		}
	}
	if !hostedFound {
		t.Fatalf("shared Resolve did not retain hosted coordinate separately from installed alias: %#v", result.Resolved)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lockStatAfter, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lockAfter, lockData) || lockStatAfter.Mode() != lockBefore.Mode() || !lockStatAfter.ModTime().Equal(lockBefore.ModTime()) {
		t.Fatal("inventory resolution changed owning repository lock bytes or metadata")
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown telemetry: %v", err)
	}
	for _, name := range []string{string(tmcfg.InventorySnapshotName), string(tmcfg.InventoryRegistryName), string(tmcfg.InventoryResourceName)} {
		if !exporter.hasMetric(name) {
			t.Errorf("shared Resolve did not emit %q", name)
		}
	}
	if exporter.hasMetric(string(tmcfg.InventoryOmissionsName)) {
		t.Fatal("shared Resolve omitted the mixed inventory")
	}
}

func TestInventoryPanicWarningIsBounded(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	func() {
		defer warnInventoryPanic()
		panic("private user value")
	}()
	got := logs.String()
	if !strings.Contains(got, "configuration inventory recording panicked") {
		t.Fatalf("panic warning missing: %q", got)
	}
	if strings.Contains(got, "private user value") {
		t.Fatalf("panic warning leaked recovered value: %q", got)
	}
}

func TestSilentSyncReportsPostMutationInventory(t *testing.T) {
	exporter := newPipelineSpanExporter(t)
	ctx, tel := setupPipelineTelemetry(t, exporter)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	registryRoot := t.TempDir()
	writeSkill(t, registryRoot, "post-mutation", "test", nil)
	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  registry: {directory: %q, source_harness: claude}\nresources:\n  registry: [skill/post-mutation]\n", registryRoot))

	if err := silentSync(ctx, mustWorkspace(t, repoRoot)); err != nil {
		t.Fatalf("silentSync: %v", err)
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown telemetry: %v", err)
	}
	if !exporter.hasMetric(string(tmcfg.InventorySnapshotName)) || !exporter.hasMetric(string(tmcfg.InventoryResourceName)) {
		t.Fatalf("post-mutation silentSync metrics = %v", exporter.metricNames)
	}
}

func TestRecordResolvedInventoryExportsOmission(t *testing.T) {
	exporter := newPipelineSpanExporter(t)
	ctx, tel := setupPipelineTelemetry(t, exporter)
	result := inventoryResolveResult()
	result.Resolved[0].Name = strings.Repeat("x", telemetry.MaxInventoryAttributeBytes+1)
	recordResolvedInventory(ctx, result)
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown telemetry: %v", err)
	}
	if !exporter.hasMetric(string(tmcfg.InventoryOmissionsName)) {
		t.Fatal("inventory omission metric was not exported")
	}
}

func TestRecordResolvedInventorySkipsConstructionUnlessDetailed(t *testing.T) {
	for _, test := range []struct {
		level       string
		wantOmitted bool
	}{
		{level: tmcfg.LevelBasic},
		{level: tmcfg.LevelDetailed, wantOmitted: true},
	} {
		t.Run(test.level, func(t *testing.T) {
			exporter := newPipelineSpanExporter(t)
			ctx, tel := setupPipelineTelemetryAtLevel(t, exporter, test.level)
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(previous)

			recordResolvedInventory(ctx, ResolveResult{})
			if err := tel.Shutdown(context.Background()); err != nil {
				t.Fatalf("shutdown telemetry: %v", err)
			}
			if got := strings.Contains(logs.String(), "configuration inventory omitted"); got != test.wantOmitted {
				t.Fatalf("inventory omission logged = %v, want %v", got, test.wantOmitted)
			}
		})
	}
}

func TestBuildInventorySnapshotRepresentsEffectiveResolveResult(t *testing.T) {
	result := inventoryResolveResult()

	snapshot, omission, ok := BuildInventorySnapshot(result)
	if !ok || omission != "" {
		t.Fatalf("expected complete inventory snapshot, omission=%q", omission)
	}

	if snapshot.Harnesses != "claude,factory" || snapshot.HarnessScope != "local" {
		t.Fatalf("harness inventory = %q/%q, want sorted harnesses and local source", snapshot.Harnesses, snapshot.HarnessScope)
	}
	wantRegistries := []telemetry.InventoryRegistry{
		{ID: "github.com/adyen/alpha", Location: "https://github.com/Adyen/alpha.git", Ref: "main", ConfigScope: "organization", Required: true},
		{ID: "github.com/adyen/beta", Location: "ssh://github.com/Adyen/beta.git", Path: "catalog", Ref: "v1", ConfigScope: "user", Default: true},
		{ID: "github.com/adyen/delta", Location: "https://github.com/Adyen/delta", ConfigScope: "local"},
		{ID: "github.com/adyen/gamma", Location: "github.com:Adyen/gamma.git", ConfigScope: "repo"},
	}
	if !reflect.DeepEqual(snapshot.Registries, wantRegistries) {
		t.Fatalf("registries = %#v, want %#v", snapshot.Registries, wantRegistries)
	}
	wantResources := []telemetry.InventoryResource{
		{RegistryID: "github.com/adyen/alpha", RegistryLocation: "https://github.com/Adyen/alpha.git", RegistryRef: "main", Kind: "command", Name: "deploy", ConfigScope: "organization", Required: true},
		{RegistryID: "github.com/adyen/beta", RegistryLocation: "ssh://github.com/Adyen/beta.git", RegistryPath: "catalog", RegistryRef: "v1", Kind: "skill", Name: "kept-after-filter", ConfigScope: "repo"},
	}
	if !reflect.DeepEqual(snapshot.Resources, wantResources) {
		t.Fatalf("resources = %#v, want only resolved resources %#v", snapshot.Resources, wantResources)
	}
	if matched, _ := regexp.MatchString(`^[0-9a-f]{64}$`, snapshot.Fingerprint); !matched {
		t.Fatalf("fingerprint %q is not lowercase SHA-256", snapshot.Fingerprint)
	}
}

// spec: telemetry/fingerprints-and-observation-order-are-stable
func TestBuildInventorySnapshotDeterministicAcrossOrdering(t *testing.T) {
	first := inventoryResolveResult()
	second := inventoryResolveResult()
	reverse(first.TargetHarnesses)
	reverse(first.MergedCfg.Registries)
	reverse(first.Resolved)

	gotFirst, firstOmission, ok := BuildInventorySnapshot(first)
	if !ok || firstOmission != "" {
		t.Fatalf("first snapshot omitted: %q", firstOmission)
	}
	gotSecond, secondOmission, ok := BuildInventorySnapshot(second)
	if !ok || secondOmission != "" {
		t.Fatalf("second snapshot omitted: %q", secondOmission)
	}
	if !reflect.DeepEqual(gotFirst, gotSecond) {
		t.Fatalf("ordering changed canonical snapshot:\nfirst=%#v\nsecond=%#v", gotFirst, gotSecond)
	}
}

func TestBuildInventorySnapshotFingerprintChangesForRepresentedFields(t *testing.T) {
	base := inventoryResolveResult()
	baseSnapshot, omission, ok := BuildInventorySnapshot(base)
	if !ok || omission != "" {
		t.Fatalf("base snapshot omitted: %q", omission)
	}

	tests := map[string]func(*ResolveResult){
		"harness":           func(r *ResolveResult) { r.TargetHarnesses[0] = hcc.TargetCursor },
		"harness source":    func(r *ResolveResult) { r.TargetHarnessSource = config.RepoCommitted },
		"registry identity": func(r *ResolveResult) { r.MergedCfg.Registries[0].URL = "https://gitlab.com/Adyen/alpha.git" },
		"registry path":     func(r *ResolveResult) { r.MergedCfg.Registries[1].Path = "other" },
		"registry ref":      func(r *ResolveResult) { r.MergedCfg.Registries[0].Ref = "v2" },
		"registry scope":    func(r *ResolveResult) { r.MergedCfg.Registries[0].Source = config.UserPersonal },
		"registry required": func(r *ResolveResult) { r.MergedCfg.Registries[0].Required = false },
		"registry default":  func(r *ResolveResult) { r.MergedCfg.Registries[1].Default = false },
		"resource registry": func(r *ResolveResult) { r.Resolved[0].RegistryURL = "https://github.com/Adyen/other.git" },
		"resource path":     func(r *ResolveResult) { r.Resolved[0].RegistryPath = "other" },
		"resource ref":      func(r *ResolveResult) { r.Resolved[0].Ref = "v2" },
		"resource kind":     func(r *ResolveResult) { r.Resolved[0].Kind = ir.KindHook },
		"resource name":     func(r *ResolveResult) { r.Resolved[0].Name = "other" },
		"resource scope":    func(r *ResolveResult) { r.Resolved[0].Source = config.UserPersonal },
		"resource required": func(r *ResolveResult) { r.Resolved[0].Required = true },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := inventoryResolveResult()
			mutate(&changed)
			snapshot, omission, ok := BuildInventorySnapshot(changed)
			if !ok || omission != "" {
				t.Fatalf("changed snapshot omitted: %q", omission)
			}
			if snapshot.Fingerprint == baseSnapshot.Fingerprint {
				t.Fatalf("represented %s did not change fingerprint %q", name, snapshot.Fingerprint)
			}
		})
	}
}

// spec: telemetry/inventory-sanitizes-registry-identity
func TestBuildInventorySnapshotRedactsLocationsAndNormalizesGitIdentity(t *testing.T) {
	result := ResolveResult{
		MergedCfg: &config.Config{Registries: []config.RegistryEntry{
			{URL: " /Users/alexander/private/catalog ", Path: "secret/subpath", Ref: "private", Source: config.RepoCommitted},
			{URL: "file:///Users/alexander/also-private", Path: "secret", Source: config.UserPersonal},
			{URL: "git@github.com:Adyen/example.git", Source: config.Organization},
			{URL: "https://token@GITHUB.COM/adyen/example/?auth=secret#fragment", Source: config.RepoPersonal},
		}},
		Resolved: []resolver.ResolvedArtifact{{
			Entry: catalog.Entry{Kind: ir.KindSkill, Name: "local-skill"}, RegistryURL: "/Users/alexander/private/catalog",
			RegistryPath: "secret/subpath", Ref: "private", Source: config.RepoCommitted,
		}},
	}

	snapshot, omission, ok := BuildInventorySnapshot(result)
	if !ok || omission != "" {
		t.Fatalf("snapshot omitted: %q", omission)
	}
	if snapshot.Registries[0].ID != "github.com/adyen/example" || snapshot.Registries[1].ID != "github.com/adyen/example" {
		t.Fatalf("equivalent Git transports have different identities: %#v", snapshot.Registries)
	}
	serialized := fmt.Sprintf("%#v", snapshot)
	for _, secret := range []string{"alexander", "private", "secret", "token", "auth", "fragment"} {
		if strings.Contains(strings.ToLower(serialized), secret) {
			t.Fatalf("snapshot leaked %q: %s", secret, serialized)
		}
	}
	localCount := 0
	for _, registry := range snapshot.Registries {
		if registry.ID == "local" {
			localCount++
			if registry.Location != "local" || registry.Path != "" || registry.Ref != "" {
				t.Fatalf("local registry was not fully redacted: %#v", registry)
			}
		}
	}
	if localCount != 2 {
		t.Fatalf("local registry count = %d, want 2", localCount)
	}
	if got := snapshot.Resources[0]; got.RegistryID != "local" || got.RegistryLocation != "local" || got.RegistryPath != "" || got.RegistryRef != "" {
		t.Fatalf("local resource registry was not fully redacted: %#v", got)
	}
}

func TestBuildInventorySnapshotTreatsExplicitFilesystemSyntaxAsLocal(t *testing.T) {
	for _, location := range []string{".", "..", "./repo.git", "../repo.git", "~/repo.git", "/tmp/repo.git", "file:///tmp/repo.git", "FILE:///Users/private/repo.git", "FiLe:/Users/private/repo.git", "file://localhost/Users/private/repo.git", "file:///Users/%70rivate/repo.git"} {
		t.Run(location, func(t *testing.T) {
			result := ResolveResult{
				MergedCfg: &config.Config{Registries: []config.RegistryEntry{{URL: location, Path: "private", Ref: "secret"}}},
				Resolved:  []resolver.ResolvedArtifact{{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "safe"}, RegistryURL: location, RegistryPath: "private", Ref: "secret"}},
			}
			snapshot, omission, ok := BuildInventorySnapshot(result)
			if !ok || omission != "" {
				t.Fatalf("local location omitted: reason=%q", omission)
			}
			if got := snapshot.Registries[0]; got.ID != "local" || got.Location != "local" || got.Path != "" || got.Ref != "" {
				t.Fatalf("registry = %#v, want fully redacted local", got)
			}
			if got := snapshot.Resources[0]; got.RegistryID != "local" || got.RegistryLocation != "local" || got.RegistryPath != "" || got.RegistryRef != "" {
				t.Fatalf("resource = %#v, want fully redacted local", got)
			}
		})
	}

	result := ResolveResult{MergedCfg: &config.Config{Registries: []config.RegistryEntry{{URL: "git@github.com:Adyen/repo.git"}}}}
	snapshot, omission, ok := BuildInventorySnapshot(result)
	if !ok || omission != "" {
		t.Fatalf("SCP remote omitted: reason=%q", omission)
	}
	if got := snapshot.Registries[0]; got.ID != "github.com/adyen/repo" || got.Location == "local" {
		t.Fatalf("SCP remote misclassified: %#v", got)
	}
}

func TestBuildInventorySnapshotRejectsUnsafeRegistryLocations(t *testing.T) {
	for _, location := range []string{
		"token@secret@github.com:Adyen/repo.git",
		"https://token@github.com/Adyen/%zz.git?secret=hidden",
		"FILE:///Users/private/%zz.git",
		"https://github.com/Adyen/repo.git\nprivate",
		"private/repo.git",
	} {
		t.Run(location, func(t *testing.T) {
			result := ResolveResult{MergedCfg: &config.Config{Registries: []config.RegistryEntry{{URL: location, Path: "private", Ref: "secret"}}}}
			snapshot, omission, ok := BuildInventorySnapshot(result)
			if ok || omission != telemetry.InventoryOmissionInvalid || !reflect.DeepEqual(snapshot, telemetry.InventorySnapshot{}) {
				t.Fatalf("unsafe location result = ok:%v omission:%q snapshot:%#v", ok, omission, snapshot)
			}
		})
	}
}

func TestBuildInventorySnapshotClassifiesOmissionsWithoutPartialData(t *testing.T) {
	tests := map[string]struct {
		mutate func(*ResolveResult)
		want   telemetry.InventoryOmissionReason
	}{
		"registry count": {
			mutate: func(result *ResolveResult) {
				result.MergedCfg.Registries = make([]config.RegistryEntry, telemetry.MaxInventoryRegistries+1)
			},
			want: telemetry.InventoryOmissionTooLarge,
		},
		"resource count": {
			mutate: func(result *ResolveResult) {
				result.Resolved = make([]resolver.ResolvedArtifact, telemetry.MaxInventoryResources+1)
			},
			want: telemetry.InventoryOmissionTooLarge,
		},
		"attribute bytes": {
			mutate: func(result *ResolveResult) {
				result.Resolved[0].Name = strings.Repeat("é", telemetry.MaxInventoryAttributeBytes/2+1)
			},
			want: telemetry.InventoryOmissionTooLarge,
		},
		"invalid location": {
			mutate: func(result *ResolveResult) { result.MergedCfg.Registries[0].URL = "" },
			want:   telemetry.InventoryOmissionInvalid,
		},
		"invalid UTF-8 attribute": {
			mutate: func(result *ResolveResult) { result.MergedCfg.Registries[0].Ref = "\xff" },
			want:   telemetry.InventoryOmissionInvalid,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			result := inventoryResolveResult()
			test.mutate(&result)
			snapshot, omission, ok := BuildInventorySnapshot(result)
			if ok || omission != test.want {
				t.Fatalf("result = ok:%v omission:%q snapshot:%#v, want omission %q", ok, omission, snapshot, test.want)
			}
			if !reflect.DeepEqual(snapshot, telemetry.InventorySnapshot{}) {
				t.Fatalf("omission returned partial snapshot: %#v", snapshot)
			}
		})
	}
}

// spec: telemetry/resolution-and-export-failures-do-not-become-inventory-success
func TestBuildInventorySnapshotSuppressesHardErrors(t *testing.T) {
	result := inventoryResolveResult()
	result.Warnings = []error{errors.New("soft warning")}
	if _, omission, ok := BuildInventorySnapshot(result); !ok || omission != "" {
		t.Fatalf("soft warnings suppressed snapshot: %q", omission)
	}
	result.Errors = []error{errors.New("hard error")}
	if snapshot, omission, ok := BuildInventorySnapshot(result); ok || omission != "" {
		t.Fatalf("hard error result = ok:%v omission:%q snapshot:%#v", ok, omission, snapshot)
	}
}

func inventoryResolveResult() ResolveResult {
	return ResolveResult{
		MergedCfg: &config.Config{Registries: []config.RegistryEntry{
			{URL: "https://github.com/Adyen/alpha.git", Ref: "main", Required: true, Source: config.Organization},
			{URL: "ssh://github.com/Adyen/beta.git", Path: "catalog", Ref: "v1", Default: true, Source: config.UserPersonal},
			{URL: "git@github.com:Adyen/gamma.git", Source: config.RepoCommitted},
			{URL: "https://github.com/Adyen/delta", Source: config.RepoPersonal},
		}},
		Resolved: []resolver.ResolvedArtifact{
			{Entry: catalog.Entry{Kind: ir.KindSkill, Name: "kept-after-filter"}, RegistryURL: "ssh://github.com/Adyen/beta.git", RegistryPath: "catalog", Ref: "v1", Source: config.RepoCommitted},
			{Entry: catalog.Entry{Kind: ir.KindCommand, Name: "deploy"}, RegistryURL: "https://github.com/Adyen/alpha.git", Ref: "main", Source: config.Organization, Required: true},
		},
		TargetHarnesses:     []hcc.Target{hcc.TargetFactory, hcc.TargetClaude},
		TargetHarnessSource: config.RepoPersonal,
	}
}

func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
