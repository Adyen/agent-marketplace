package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func legacyWarnings(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var warnings []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("slog record is not JSON: %q: %v", line, err)
		}
		if record["code"] == "config.legacy-deprecated" {
			warnings = append(warnings, record)
		}
	}
	return warnings
}

func mutateIncludeSkill(m *LayerMutation) error {
	binding, err := m.EnsureRegistry(config.RegistryEntry{URL: "https://example.com/registry.git", Ref: "main"})
	if err != nil {
		return err
	}
	return m.Apply(binding, config.SelectionChange{Operation: config.SelectionInclude, Locators: []config.ResourceLocator{{Kind: ir.KindSkill, Name: "one"}}})
}

func TestEditLayerAtPathWarnsOnceOnLegacyMutation(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := config.ProjectConfigPath(repoRoot)
	writeFile(t, configPath, "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	logs := captureSlog(t)

	if err := EditLayerAtPath(config.LayerTarget{Source: config.RepoCommitted, StagedPath: configPath, DeclaringPath: configPath}, mutateIncludeSkill); err != nil {
		t.Fatal(err)
	}
	warnings := legacyWarnings(t, logs)
	if len(warnings) != 1 {
		t.Fatalf("legacy warnings = %d, want 1: %s", len(warnings), logs)
	}
	record := warnings[0]
	if record["path"] != configPath {
		t.Fatalf("warning path = %v, want %s", record["path"], configPath)
	}
	if record["source"] != config.RepoCommitted.String() {
		t.Fatalf("warning source = %v, want %s", record["source"], config.RepoCommitted)
	}
	if record["next_command"] != "agent-marketplace migrate --scope repo" {
		t.Fatalf("warning next_command = %v", record["next_command"])
	}
}

func TestEditLayerAtPathSkipsWarningForV4AndNoop(t *testing.T) {
	repoRoot := t.TempDir()
	v4Path := filepath.Join(repoRoot, "v4.yaml")
	writeFile(t, v4Path, "apiVersion: agent-marketplace/v4\n")
	legacyPath := filepath.Join(repoRoot, "legacy.yaml")
	writeFile(t, legacyPath, "apiVersion: agent-marketplace/v2\n")
	logs := captureSlog(t)

	if err := EditLayerAtPath(config.LayerTarget{Source: config.RepoCommitted, StagedPath: v4Path, DeclaringPath: v4Path}, mutateIncludeSkill); err != nil {
		t.Fatal(err)
	}
	if err := EditLayerAtPath(config.LayerTarget{Source: config.RepoCommitted, StagedPath: legacyPath, DeclaringPath: legacyPath}, func(*LayerMutation) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if warnings := legacyWarnings(t, logs); len(warnings) != 0 {
		t.Fatalf("legacy warnings = %d, want 0: %s", len(warnings), logs)
	}
}

func TestMutateMarketplaceLayerWarnsOnceOnLegacyMutation(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeFile(t, configPath, "apiVersion: agent-marketplace/v2\n")
	logs := captureSlog(t)

	digest := "sha256:" + strings.Repeat("a", 64)
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Alias: "public"}
	pin := config.MarketplaceResourcePin{RegistryID: "src", Kind: "skill", Name: "review", Revision: digest, Digest: digest}
	if _, err := MutateMarketplaceLayer(context.Background(), repoRoot, "", configPath, config.RepoCommitted, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(entry, []config.MarketplaceResourcePin{pin})
	}, func() ([]error, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	warnings := legacyWarnings(t, logs)
	if len(warnings) != 1 {
		t.Fatalf("legacy warnings = %d, want 1: %s", len(warnings), logs)
	}
	if warnings[0]["next_command"] != "agent-marketplace migrate --scope repo" {
		t.Fatalf("warning next_command = %v", warnings[0]["next_command"])
	}
	if warnings[0]["path"] != configPath {
		t.Fatalf("warning path = %v, want %s", warnings[0]["path"], configPath)
	}
}

func TestMutateMarketplaceLayerSkipsWarningOnEndpointNoop(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeFile(t, configPath, "apiVersion: agent-marketplace/v2\n")
	entry := config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Alias: "public"}
	if err := config.EnsureMarketplaceEntryInConfig(configPath, entry); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	frozen := time.Unix(1700000000, 0)
	if err := os.Chtimes(configPath, frozen, frozen); err != nil {
		t.Fatal(err)
	}
	logs := captureSlog(t)

	if _, err := MutateMarketplaceLayer(context.Background(), repoRoot, "", configPath, config.RepoCommitted, func(m *MarketplaceLayerMutation) error {
		return m.EnsureEndpoint(entry)
	}, func() ([]error, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if warnings := legacyWarnings(t, logs); len(warnings) != 0 {
		t.Fatalf("legacy warnings = %d, want 0: %s", len(warnings), logs)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("config bytes changed on endpoint no-op:\nbefore %q\nafter %q", before, after)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(frozen) {
		t.Fatalf("config mtime = %v, want frozen %v", info.ModTime(), frozen)
	}
	if info.Mode().Perm() != beforeInfo.Mode().Perm() {
		t.Fatalf("config mode = %v, want %v", info.Mode().Perm(), beforeInfo.Mode().Perm())
	}
}

func TestMutateMarketplaceLayerSkipsWarningWithoutMutation(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeFile(t, configPath, "apiVersion: agent-marketplace/v2\n")
	logs := captureSlog(t)

	if _, err := MutateMarketplaceLayer(context.Background(), repoRoot, "", configPath, config.RepoCommitted, func(*MarketplaceLayerMutation) error {
		return nil
	}, func() ([]error, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if warnings := legacyWarnings(t, logs); len(warnings) != 0 {
		t.Fatalf("legacy warnings = %d, want 0: %s", len(warnings), logs)
	}
	if info, err := os.Stat(configPath); err != nil || info.Size() == 0 {
		t.Fatalf("config stat = %v/%v", info, err)
	}
}
