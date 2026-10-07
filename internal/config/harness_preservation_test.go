package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func harnessPreservationBody(schema string) string {
	if schema == "v4" {
		return "apiVersion: agent-marketplace/v4\nharnesses: [factory, future-harness]\nsources: {}\nresources: {}\n"
	}
	return "apiVersion: agent-marketplace/" + schema + "\nharnesses: [factory, future-harness]\n"
}

func writeUserRoot(t *testing.T, body string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}
	writeFile(t, path, body)
	return path
}

// spec: config-layering/existing-unsupported-identifier-is-retained-without-materialization-authority
func TestReaderRetainsUnsupportedTopLevelHarnessIDs(t *testing.T) {
	for _, schema := range []string{"v2", "v3", "v4"} {
		t.Run(schema, func(t *testing.T) {
			path := writeUserRoot(t, harnessPreservationBody(schema))

			cfg, err := LoadGlobalConfig()
			if err != nil {
				t.Fatalf("LoadGlobalConfig rejected an authored harness identifier: %v", err)
			}
			if !equalStrings(cfg.Harnesses, []string{"factory", "future-harness"}) {
				t.Fatalf("reader dropped identifiers: %v", cfg.Harnesses)
			}

			settings, err := LoadUserSettings()
			if err != nil {
				t.Fatalf("LoadUserSettings: %v", err)
			}
			if !equalStrings(settings.Harnesses, []string{"factory", "future-harness"}) {
				t.Fatalf("settings dropped identifiers: %v", settings.Harnesses)
			}

			cfg2, err := LoadConfigFromPath(path)
			if err != nil {
				t.Fatalf("LoadConfigFromPath: %v", err)
			}
			if !equalStrings(cfg2.Harnesses, []string{"factory", "future-harness"}) {
				t.Fatalf("direct reader dropped identifiers: %v", cfg2.Harnesses)
			}
		})
	}
}

// spec: config-layering/existing-unsupported-identifier-is-retained-without-materialization-authority
func TestReadIntentWarnsOnUnsupportedHarnessIDs(t *testing.T) {
	for _, schema := range []string{"v2", "v3", "v4"} {
		t.Run(schema, func(t *testing.T) {
			dir := t.TempDir()
			orgPath := filepath.Join(dir, "org.yaml")
			writeFile(t, orgPath, "apiVersion: agent-marketplace/v2\n")
			userPath := filepath.Join(dir, "user.yaml")
			writeFile(t, userPath, harnessPreservationBody(schema))
			opts := guiEffectiveOptions(dir, orgPath, userPath,
				filepath.Join(dir, "missing-repo.yaml"), filepath.Join(dir, "missing-local.yaml"))

			ec := loadEffectiveForTest(context.Background(), opts)
			if len(ec.Errors) != 0 {
				t.Fatalf("LoadEffective errors: %v", ec.Errors)
			}
			var warn *UnsupportedHarnessWarning
			found := false
			for _, w := range ec.Warnings {
				if errors.As(w, &warn) {
					found = true
					if warn.Code != "config.harness.unsupported" {
						t.Fatalf("warning code = %q, want config.harness.unsupported", warn.Code)
					}
				}
			}
			if !found {
				t.Fatalf("missing unsupported-harness warning in %v", ec.Warnings)
			}
			if ec.User.Config == nil || !equalStrings(ec.User.Config.Harnesses, []string{"factory", "future-harness"}) {
				t.Fatalf("bound user config lost identifiers: %#v", ec.User.Config)
			}
			if !equalStrings(ec.TargetHarnesses, []string{"factory", "future-harness"}) {
				t.Fatalf("effective raw target identifiers = %v", ec.TargetHarnesses)
			}
		})
	}
}

// spec: session-interactions/save-selected-harnesses
func TestSetHarnessesInConfigPreservesExistingUnsupportedIDs(t *testing.T) {
	for _, schema := range []string{"v2", "v3", "v4"} {
		t.Run(schema, func(t *testing.T) {
			path := writeUserRoot(t, harnessPreservationBody(schema))
			if err := SetHarnessesInConfig(path, []string{"claude", "future-harness"}); err != nil {
				t.Fatalf("save of an already-present identifier rejected: %v", err)
			}
			cfg, err := LoadConfigFromPath(path)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if !equalStrings(cfg.Harnesses, []string{"claude", "future-harness"}) {
				t.Fatalf("harnesses = %v, want [claude future-harness]", cfg.Harnesses)
			}
		})
	}
}

// spec: session-interactions/save-selected-harnesses
func TestSetHarnessesInConfigDeselectRemovesUnsupportedID(t *testing.T) {
	path := writeUserRoot(t, harnessPreservationBody("v4"))
	if err := SetHarnessesInConfig(path, []string{"factory"}); err != nil {
		t.Fatalf("deselect save rejected: %v", err)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !equalStrings(cfg.Harnesses, []string{"factory"}) {
		t.Fatalf("deselected identifier was reinserted: %v", cfg.Harnesses)
	}
}

// spec: session-interactions/newly-introduced-unsupported-harness-target-is-refused
func TestSetHarnessesInConfigRejectsNewlyIntroducedUnsupportedIDs(t *testing.T) {
	for _, schema := range []string{"v2", "v3", "v4"} {
		t.Run(schema, func(t *testing.T) {
			path := writeUserRoot(t, harnessPreservationBody(schema))
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := SetHarnessesInConfig(path, []string{"factory", "other-unknown"}); err == nil {
				t.Fatal("newly introduced unsupported identifier was accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(original) {
				t.Fatalf("refused save changed bytes:\n%s", after)
			}
		})
	}

	t.Run("missing root refuses unsupported init", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		path, err := GlobalConfigPath()
		if err != nil {
			t.Fatalf("GlobalConfigPath: %v", err)
		}
		if err := SetHarnessesInConfig(path, []string{"factory", "future-harness"}); err == nil {
			t.Fatal("unsupported identifier accepted into a new root")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("refused save created %q", path)
		}
	})
}

// spec: session-interactions/newly-introduced-unsupported-harness-target-is-refused
func TestSetHarnessesInConfigRejectsScalarLookalikeIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		name        string
		harnessYAML string
		proposed    []string
	}{
		{name: "numeric original", harnessYAML: "harnesses: [factory, 42]\n", proposed: []string{"factory", "42"}},
		{name: "bool original", harnessYAML: "harnesses: [factory, true]\n", proposed: []string{"factory", "true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfigTemp(t, "apiVersion: agent-marketplace/v2\n"+tc.harnessYAML)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := SetHarnessesInConfig(path, tc.proposed); err == nil {
				t.Fatalf("non-string original entry authorized unsupported string save %v", tc.proposed)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(original) {
				t.Fatalf("refused save changed bytes:\n%s", after)
			}
		})
	}

	t.Run("string unsupported original is a valid save", func(t *testing.T) {
		path := writeConfigTemp(t, "apiVersion: agent-marketplace/v2\nharnesses: [factory, future-harness]\n")
		if err := SetHarnessesInConfig(path, []string{"factory", "future-harness"}); err != nil {
			t.Fatalf("already-present unsupported string identifier rejected: %v", err)
		}
		cfg, err := LoadConfigFromPath(path)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if !equalStrings(cfg.Harnesses, []string{"factory", "future-harness"}) {
			t.Fatalf("harnesses = %v", cfg.Harnesses)
		}
	})
}

// spec: session-interactions/initial-configuration-rejects-unsupported-harness-targets
func TestInitializeUnsupportedHarnessDoesNotWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}
	if _, err := InitializeUserPersonalAtPath(path, []string{"factory", "future-harness"}); err == nil {
		t.Fatal("unsupported identifier accepted into a new root")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("refused init created %q", path)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("refused init left output under HOME: %v", entries)
	}
	if _, err := InitializeUserPersonalAtPath(path, []string{"factory"}); err != nil {
		t.Fatalf("supported init control failed: %v", err)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("reload after init: %v", err)
	}
	if !equalStrings(cfg.Harnesses, []string{"factory"}) {
		t.Fatalf("initialized harnesses = %v", cfg.Harnesses)
	}
}

// spec: config-layering/existing-unsupported-identifier-is-retained-without-materialization-authority
func TestReaderRejectsNonStringTopLevelHarnessItems(t *testing.T) {
	bodies := map[string]string{
		"integer item":  "harnesses: [factory, 42]\n",
		"bool item":     "harnesses: [factory, true]\n",
		"null item":     "harnesses: [factory, null]\n",
		"empty item":    "harnesses: [factory, \"\"]\n",
		"blank item":    "harnesses: [factory, \"  \"]\n",
		"scalar list":   "harnesses: factory\n",
		"valid strings": "harnesses: [factory, future-harness]\n",
	}
	for name, harnessYAML := range bodies {
		for _, schema := range []string{"v2", "v3", "v4"} {
			t.Run(name+"/"+schema, func(t *testing.T) {
				body := fmt.Sprintf("apiVersion: agent-marketplace/%s\n%s", schema, harnessYAML)
				if schema == "v4" {
					body += "sources: {}\nresources: {}\n"
				}
				cfg, err := LoadConfigFromPath(writeConfigTemp(t, body))
				if name == "valid strings" {
					if err != nil {
						t.Fatalf("supported+unknown string list rejected: %v", err)
					}
					if !equalStrings(cfg.Harnesses, []string{"factory", "future-harness"}) {
						t.Fatalf("harnesses = %v", cfg.Harnesses)
					}
					return
				}
				if err == nil {
					t.Fatalf("%s harnesses item accepted", name)
				}
			})
		}
	}
}

func writeConfigTemp(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, body)
	return path
}

// spec: config-layering/existing-unsupported-identifier-is-retained-without-materialization-authority
func TestReaderStillRejectsUnsupportedPerEntryHarnesses(t *testing.T) {
	t.Run("legacy entry harnesses", func(t *testing.T) {
		_, errs := loadV3ConfigWithAPIVersion([]byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: ./registry\n    harnesses: [future-harness]\n"), userLayer, true, true)
		if len(errs) == 0 {
			t.Fatal("unknown per-entry harness accepted")
		}
		joined := errors.Join(errs...).Error()
		if !strings.Contains(joined, "registries[0].harnesses[0]") || !strings.Contains(joined, "future-harness") {
			t.Fatalf("legacy per-entry error = %q, want field path and identifier", joined)
		}
	})
	t.Run("legacy entry known control", func(t *testing.T) {
		cfg, errs := loadV3ConfigWithAPIVersion([]byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: ./registry\n    harnesses: [factory]\n"), userLayer, true, true)
		if len(errs) != 0 {
			t.Fatalf("known per-entry harness rejected: %v", errs)
		}
		if cfg == nil || len(cfg.Registries) != 1 || !equalStrings(cfg.Registries[0].Harnesses, []string{"factory"}) {
			t.Fatalf("registries = %#v", cfg.Registries)
		}
	})
	t.Run("legacy entry source_harness", func(t *testing.T) {
		_, errs := loadV3ConfigWithAPIVersion([]byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: ./registry\n    source_harness: future-harness\n"), userLayer, true, true)
		if len(errs) == 0 {
			t.Fatal("unknown source_harness accepted")
		}
		joined := errors.Join(errs...).Error()
		if !strings.Contains(joined, "registries[0].source_harness") || !strings.Contains(joined, "future-harness") {
			t.Fatalf("legacy source_harness error = %q, want field path and identifier", joined)
		}
	})
	t.Run("legacy entry known source_harness control", func(t *testing.T) {
		_, errs := loadV3ConfigWithAPIVersion([]byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: ./registry\n    source_harness: factory\n"), userLayer, true, true)
		if len(errs) != 0 {
			t.Fatalf("known source_harness rejected: %v", errs)
		}
	})
	t.Run("v4 source harnesses", func(t *testing.T) {
		body := "apiVersion: agent-marketplace/v4\nharnesses: [factory, future-harness]\nsources:\n  agents:\n    directory: ./registry\nresources:\n  agents:\n    harnesses: [future-harness]\n"
		_, err := ParseV4Config([]byte(body), "config.yaml", true)
		var validation *V4ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("error = %T %v, want V4ValidationError", err, err)
		}
		if validation.Code != "v4.harness" || validation.Path != "resources.agents.harnesses[0]" {
			t.Fatalf("v4 rejection = %s %s, want v4.harness resources.agents.harnesses[0]", validation.Code, validation.Path)
		}
	})
	t.Run("v4 source known harnesses control", func(t *testing.T) {
		body := "apiVersion: agent-marketplace/v4\nharnesses: [factory, future-harness]\nsources:\n  agents:\n    directory: ./registry\nresources:\n  agents:\n    harnesses: [factory]\n"
		if _, err := ParseV4Config([]byte(body), "config.yaml", true); err != nil {
			t.Fatalf("known per-source harness rejected: %v", err)
		}
	})
	t.Run("v4 source_harness", func(t *testing.T) {
		body := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  agents:\n    directory: ./registry\n    source_harness: future-harness\nresources: {}\n"
		_, err := ParseV4Config([]byte(body), "config.yaml", true)
		var validation *V4ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("error = %T %v, want V4ValidationError", err, err)
		}
		if validation.Code != "v4.harness" || !strings.Contains(validation.Path, "source_harness") {
			t.Fatalf("v4 source_harness rejection = %s %s", validation.Code, validation.Path)
		}
	})
}
