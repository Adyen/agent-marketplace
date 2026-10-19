package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func assertFileUnchanged(t *testing.T, path string, before []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(before) {
		t.Fatalf("file changed: %q %v", got, err)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s absent: %v", path, err)
	}
}

// spec: config-layering/v4-roots-declare-marketplace-sources-and-resources
func TestV4MarketplaceAuthoredSettingsAndAcquisitionEvidence(t *testing.T) {
	fixture := newV4OnlyFixture(t)
	path := fixture.paths[UserPersonal]
	v4OnlyWriteFixture(t, path, v4MarketplaceConfigYAML+"allowed_marketplaces: ['^https://market\\.example$']\nharnesses: [factory]\n")
	intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
	if len(intent.Errors) != 0 {
		t.Fatal(errors.Join(intent.Errors...))
	}
	layer := intent.Layers[UserPersonal]
	if !layer.NeedsMarketplaceLock() || len(layer.MarketplaceSources()) != 1 || len(layer.MarketplaceSources()[0].Requests) != 2 || layer.Settings().AllowedMarketplaces == nil {
		t.Fatalf("authored selections/policy lost: %#v", layer)
	}
	lockPath := filepath.Join(filepath.Dir(path), "lock.yaml")
	assertFileMissing(t, lockPath)
	if _, err := LoadConfigFromPath(path); !MissingMarketplaceEvidence(err) {
		t.Fatalf("acquired resources missing lock not refused: %v", err)
	}
	doc := v4LockFixture(t, lockfile.ScopeUser)
	data, err := lockfile.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	before := v4OnlyWriteFixture(t, lockPath, string(data))
	cfg, err := LoadConfigFromPath(path)
	if err != nil || len(cfg.Marketplaces) != 1 || len(cfg.Marketplaces[0].Resources) != 4 || cfg.Marketplaces[0].InstanceID != "market-id" {
		t.Fatalf("lock hydration = %#v %v", cfg, err)
	}
	if err := SetHarnessesInConfig(path, []string{"claude"}); err != nil {
		t.Fatal(err)
	}
	v4OnlyAssertUnchanged(t, lockPath, before)
	cfg, err = LoadConfigFromPath(path)
	if err != nil || !equalStrings(cfg.Harnesses, []string{"claude"}) {
		t.Fatalf("harness update failed: %#v %v", cfg, err)
	}
}

func TestV4SettingsEditDoesNotRequireAcquisitionLock(t *testing.T) {
	for _, deployed := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "malformed"}[deployed], func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			path := fixture.paths[UserPersonal]
			v4OnlyWriteFixture(t, path, v4MarketplaceConfigYAML+"harnesses: [factory]\n")
			lockPath := filepath.Join(filepath.Dir(path), "lock.yaml")
			if deployed {
				v4OnlyWriteFixture(t, lockPath, "malformed deployed lock\n")
			}
			if err := SetUserTelemetryLevel(path, "off"); err != nil {
				t.Fatal(err)
			}
			settings, err := LoadUserSettings()
			if err != nil || settings.Telemetry == nil || settings.Telemetry.Level != "off" {
				t.Fatalf("telemetry edit = %#v %v", settings, err)
			}
			if deployed {
				assertFileUnchanged(t, lockPath, []byte("malformed deployed lock\n"))
			} else {
				assertFileMissing(t, lockPath)
			}
		})
	}
}
