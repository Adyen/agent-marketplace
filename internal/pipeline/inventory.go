package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
)

// BuildInventorySnapshot returns a bounded omission reason rather than a partial generation.
func BuildInventorySnapshot(result ResolveResult) (telemetry.InventorySnapshot, telemetry.InventoryOmissionReason, bool) {
	if len(result.Errors) != 0 {
		return telemetry.InventorySnapshot{}, "", false
	}
	if result.MergedCfg == nil {
		return telemetry.InventorySnapshot{}, telemetry.InventoryOmissionInvalid, false
	}
	if len(result.MergedCfg.Registries) > telemetry.MaxInventoryRegistries || len(result.Resolved) > telemetry.MaxInventoryResources {
		return telemetry.InventorySnapshot{}, telemetry.InventoryOmissionTooLarge, false
	}

	harnesses := make([]string, len(result.TargetHarnesses))
	for i, target := range result.TargetHarnesses {
		harnesses[i] = string(target)
	}
	sort.Strings(harnesses)
	snapshot := telemetry.InventorySnapshot{
		Harnesses:    strings.Join(harnesses, ","),
		HarnessScope: inventoryScope(result.TargetHarnessSource),
		Registries:   make([]telemetry.InventoryRegistry, 0, len(result.MergedCfg.Registries)),
		Resources:    make([]telemetry.InventoryResource, 0, len(result.Resolved)),
	}

	for _, entry := range result.MergedCfg.Registries {
		identity, location, local, ok := telemetry.SanitizeRegistryTelemetry(entry.URL)
		if !ok {
			return telemetry.InventorySnapshot{}, telemetry.InventoryOmissionInvalid, false
		}
		registry := telemetry.InventoryRegistry{
			ID: identity, Location: location, Path: strings.TrimSpace(entry.Path), Ref: strings.TrimSpace(entry.Ref),
			ConfigScope: inventoryScope(entry.Source), Required: entry.Required, Default: entry.Default,
		}
		if local {
			registry.Path = ""
			registry.Ref = ""
		}
		snapshot.Registries = append(snapshot.Registries, registry)
	}

	for _, artifact := range result.Resolved {
		identity, location, local, ok := telemetry.SanitizeRegistryTelemetry(artifact.RegistryURL)
		if !ok {
			return telemetry.InventorySnapshot{}, telemetry.InventoryOmissionInvalid, false
		}
		resource := telemetry.InventoryResource{
			RegistryID: identity, RegistryLocation: location, RegistryPath: strings.TrimSpace(artifact.RegistryPath),
			RegistryRef: strings.TrimSpace(artifact.Ref), Kind: artifact.Kind.String(), Name: artifact.Name,
			ConfigScope: inventoryScope(artifact.Source), Required: artifact.Required,
		}
		if local {
			resource.RegistryPath = ""
			resource.RegistryRef = ""
		}
		snapshot.Resources = append(snapshot.Resources, resource)
	}

	sort.Slice(snapshot.Registries, func(i, j int) bool {
		return inventoryRegistryKey(snapshot.Registries[i]) < inventoryRegistryKey(snapshot.Registries[j])
	})
	sort.Slice(snapshot.Resources, func(i, j int) bool {
		return inventoryResourceKey(snapshot.Resources[i]) < inventoryResourceKey(snapshot.Resources[j])
	})
	if reason := telemetry.InventorySnapshotOmissionReason(snapshot); reason != "" {
		return telemetry.InventorySnapshot{}, reason, false
	}
	canonical, err := json.Marshal(struct {
		Harnesses    string                        `json:"harnesses"`
		HarnessScope string                        `json:"harness_scope"`
		Registries   []telemetry.InventoryRegistry `json:"registries"`
		Resources    []telemetry.InventoryResource `json:"resources"`
	}{snapshot.Harnesses, snapshot.HarnessScope, snapshot.Registries, snapshot.Resources})
	if err != nil {
		return telemetry.InventorySnapshot{}, telemetry.InventoryOmissionInvalid, false
	}
	digest := sha256.Sum256(canonical)
	snapshot.Fingerprint = hex.EncodeToString(digest[:])
	return snapshot, "", true
}

// RecordResolvedInventory records one already-computed successful resolution.
// It is intended for command boundaries that suppress Resolve's automatic
// inventory while deciding whether a configuration mutation will follow.
func RecordResolvedInventory(ctx context.Context, result ResolveResult) {
	recordResolvedInventory(ctx, result)
}

func recordResolvedInventory(ctx context.Context, result ResolveResult) {
	if !telemetry.InventoryEnabled(ctx) {
		return
	}
	defer warnInventoryPanic()
	if len(result.Errors) != 0 {
		return
	}
	snapshot, omission, ok := BuildInventorySnapshot(result)
	if !ok {
		if omission == "" {
			return
		}
		slog.Warn("configuration inventory omitted", "reason", string(omission), "registries", inventoryRegistryCount(result), "resources", len(result.Resolved))
		telemetry.RecordInventoryOmission(ctx, omission)
		return
	}
	telemetry.RecordInventory(ctx, snapshot)
}

func warnInventoryPanic() {
	if recover() != nil {
		slog.Warn("configuration inventory recording panicked")
	}
}

func inventoryScope(source config.Source) string {
	switch source {
	case config.Organization:
		return "organization"
	case config.UserPersonal:
		return "user"
	case config.RepoCommitted:
		return "repo"
	case config.RepoPersonal:
		return "local"
	default:
		return "unknown"
	}
}

func inventoryRegistryKey(value telemetry.InventoryRegistry) string {
	return strings.Join([]string{value.ID, value.Location, value.Path, value.Ref, value.ConfigScope, boolString(value.Required), boolString(value.Default)}, "\x00")
}

func inventoryResourceKey(value telemetry.InventoryResource) string {
	return strings.Join([]string{value.RegistryID, value.RegistryLocation, value.RegistryPath, value.RegistryRef, value.Kind, value.Name, value.ConfigScope, boolString(value.Required)}, "\x00")
}

func inventoryRegistryCount(result ResolveResult) int {
	if result.MergedCfg == nil {
		return 0
	}
	return len(result.MergedCfg.Registries)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
