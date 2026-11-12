package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestServeClaudeDeliveryStatusShowsOperatorStateWithoutContent(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	path := claudeDeliveryCommandConfig(t)
	cfg, err := mpconfig.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := mpconfig.OpenStores(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	state := meta.(store.ClaudeDeliveryStateStore)
	now := time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	for _, row := range []store.ClaudeDeliveryPlugin{
		{
			Coordinate: store.ResourceCoordinate{Marketplace: store.MarketplaceID(cfg.Instance.ID), Registry: "registry", Kind: "skill", Name: "guide"},
			PluginName: "am-registry-skill-guide", PluginID: "plugin_guide",
			TargetMarketplaceID: "marketplace_1", PublishedVersionID: "version_2",
			LatestVersionID: "version_2", ServedVersionID: "version_1",
			ContentScanStatus: "processing", Reach: "remote", InstallationPreference: "available",
			State:            store.ClaudeDeliveryStatePublished,
			LastErrorMessage: "sanitized failure", CreatedAt: now, UpdatedAt: now,
		},
		{
			Coordinate: store.ResourceCoordinate{Marketplace: store.MarketplaceID(cfg.Instance.ID), Registry: "registry", Kind: "command", Name: "retired"},
			PluginName: "am-registry-command-retired", PluginID: "plugin_retired",
			TargetMarketplaceID: "marketplace_1", PublishedVersionID: "version_old",
			ServedVersionID: "version_old", ContentScanStatus: "completed", Reach: "remote",
			InstallationPreference: "not_available", State: store.ClaudeDeliveryStatePublished,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			Coordinate: store.ResourceCoordinate{Marketplace: store.MarketplaceID(cfg.Instance.ID), Registry: "registry", Kind: "mcpserver", Name: "failed"},
			PluginName: "am-registry-mcpserver-failed", PluginID: "plugin_failed",
			TargetMarketplaceID: "marketplace_1", State: store.ClaudeDeliveryStatePublished,
			CreatedAt: now, UpdatedAt: now,
		},
	} {
		if err := state.UpsertClaudeDeliveryPublished(context.Background(), row); err != nil {
			t.Fatalf("seed delivery state: %v", err)
		}
		if row.Coordinate.Name == "retired" {
			if err := state.MarkClaudeDeliveryRetired(context.Background(), row.Coordinate, now); err != nil {
				t.Fatalf("retire delivery state: %v", err)
			}
		}
		if row.Coordinate.Name == "failed" {
			if err := state.RecordClaudeDeliveryFailure(context.Background(), row.Coordinate,
				row.PluginName, row.TargetMarketplaceID, "upload.failed", now); err != nil {
				t.Fatalf("record delivery failure: %v", err)
			}
		}
	}
	if err := meta.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := newServeCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"claude-delivery", "status", "--config", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("serve claude-delivery status: %v", err)
	}
	for _, expected := range []string{
		"registry/skill/guide", "am-registry-skill-guide", "plugin_guide",
		"published", "version_2", "version_1", "processing", "remote",
		"available", "upload.failed", "awaiting its content scan",
		"installed copies", "version history",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("status omitted %q:\n%s", expected, output.String())
		}
	}
	for _, forbidden := range []string{"sanitized failure", "sk-ant-secret", "resource-body", "hook-command", "mcp-secret"} {
		if strings.Contains(output.String(), forbidden) {
			t.Errorf("status exposed %q:\n%s", forbidden, output.String())
		}
	}
}

func TestServeClaudeDeliverySyncRequiresOnceAndNoOpsWhenDisabled(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	path := claudeDeliveryCommandConfig(t)
	cmd := newServeCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"claude-delivery", "sync", "--config", path, "--once"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("disabled sync --once: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "disabled") {
		t.Fatalf("disabled sync did not report its no-op: %s", output.String())
	}
	cfg, err := mpconfig.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := mpconfig.OpenStores(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	rows, err := meta.(store.ClaudeDeliveryStateStore).ListClaudeDeliveryPlugins(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatalf("disabled sync created delivery state: %+v %v", rows, err)
	}

	cmd = newServeCmd()
	cmd.SetArgs([]string{"claude-delivery", "sync", "--config", path})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--once") {
		t.Fatalf("sync without --once error = %v, want usage error naming --once", err)
	}
}

func claudeDeliveryCommandConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "marketplace.yaml")
	config := strings.ReplaceAll(minimalMarketplaceConfig(), "/tmp/marketplace-test.db",
		filepath.Join(filepath.Dir(path), "marketplace.db"))
	config = strings.ReplaceAll(config, "/tmp/blobs-test", filepath.Join(filepath.Dir(path), "blobs"))
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write Marketplace config: %v", err)
	}
	return path
}
