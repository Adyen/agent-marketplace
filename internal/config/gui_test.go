package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func guiConfigBody(gui string) string {
	return "apiVersion: agent-marketplace/v4\n" + gui
}

func guiEffectiveOptions(dir string, org, user, repo, local string) LoadEffectiveOptions {
	return LoadEffectiveOptions{
		RepoRoot:         dir,
		LocalConfigDir:   dir,
		OrganizationPath: org,
		GlobalPath:       user,
		RepoPath:         repo,
		LocalPath:        local,
	}
}

func guiWarningFor(t *testing.T, warnings []error, source Source) *GUIIgnoredSettingWarning {
	t.Helper()
	for _, w := range warnings {
		var gui *GUIIgnoredSettingWarning
		if errors.As(w, &gui) && gui.Source == source {
			return gui
		}
	}
	return nil
}

func organizationSettings(t *testing.T, opts LoadEffectiveOptions) Settings {
	t.Helper()
	intent := ReadIntent(context.Background(), opts)
	if len(intent.Errors) != 0 {
		t.Fatalf("ReadIntent errors: %v", intent.Errors)
	}
	return intent.Layers[Organization].Settings()
}

// spec: config-layering/organization-setting-controls-release-availability-checks
// spec: config-layering/release-availability-checks-default-to-enabled
func TestGUIOrganizationVersionCheckControlsEffectivePolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		gui  string
		want bool
	}{
		{name: "explicit true", gui: "gui:\n  version_check: true\n", want: true},
		{name: "explicit false", gui: "gui:\n  version_check: false\n", want: false},
		{name: "gui absent", gui: "", want: true},
		{name: "empty gui mapping", gui: "gui: {}\n", want: true},
		{name: "missing version_check", gui: "gui:\n", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			orgPath := filepath.Join(dir, "org.yaml")
			writeFile(t, orgPath, guiConfigBody(tc.gui))
			opts := guiEffectiveOptions(
				dir, orgPath,
				filepath.Join(dir, "missing-user.yaml"),
				filepath.Join(dir, "missing-repo.yaml"),
				filepath.Join(dir, "missing-local.yaml"),
			)

			SetTestOrganizationConfigPath(t, orgPath)
			settings, err := LoadOrganizationSettings()
			if err != nil {
				t.Fatalf("LoadOrganizationSettings: %v", err)
			}
			if got := GUIReleaseCheckEnabled(settings); got != tc.want {
				t.Fatalf("GUIReleaseCheckEnabled(settings) = %v, want %v", got, tc.want)
			}
			if got := GUIReleaseCheckEnabled(organizationSettings(t, opts)); got != tc.want {
				t.Fatalf("GUIReleaseCheckEnabled(ReadIntent settings) = %v, want %v", got, tc.want)
			}

			ec := loadEffectiveForTest(context.Background(), opts)
			if len(ec.Errors) != 0 {
				t.Fatalf("LoadEffective errors: %v", ec.Errors)
			}
			if ec.Organization.Config == nil {
				t.Fatal("organization layer did not load")
			}
			var mergedValue *bool
			if ec.Merged != nil && ec.Merged.GUI != nil {
				mergedValue = ec.Merged.GUI.VersionCheck
			}
			if tc.name == "explicit true" || tc.name == "explicit false" {
				if mergedValue == nil || *mergedValue != tc.want {
					t.Fatalf("merged version_check = %v, want %v", mergedValue, tc.want)
				}
			} else if mergedValue != nil {
				t.Fatalf("merged version_check = %v, want unset", *mergedValue)
			}
		})
	}
}

// spec: config-layering/lower-layer-gui-setting-is-ignored-with-a-warning
func TestGUILowerLayerVersionCheckIgnoredWithWarning(t *testing.T) {
	for _, layer := range []struct {
		name   string
		source Source
		field  string
		kind   layerKind
	}{
		{name: "user-personal", source: UserPersonal, field: "user", kind: userLayer},
		{name: "repository-committed", source: RepoCommitted, field: "repo", kind: repoLayer},
		{name: "personal-local", source: RepoPersonal, field: "local", kind: localLayer},
	} {
		for _, tc := range []struct {
			name    string
			orgGUI  string
			lowGUI  string
			want    bool
			warning bool
		}{
			{name: "false below organization true", orgGUI: "gui:\n  version_check: true\n", lowGUI: "gui:\n  version_check: false\n", want: true, warning: true},
			{name: "true below organization false", orgGUI: "gui:\n  version_check: false\n", lowGUI: "gui:\n  version_check: true\n", want: false, warning: true},
			{name: "false below absent organization", orgGUI: "", lowGUI: "gui:\n  version_check: false\n", want: true, warning: true},
			{name: "no lower setting", orgGUI: "gui:\n  version_check: false\n", lowGUI: "", want: false, warning: false},
		} {
			t.Run(layer.name+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				paths := map[string]string{
					"org":   filepath.Join(dir, "org.yaml"),
					"user":  filepath.Join(dir, "user.yaml"),
					"repo":  filepath.Join(dir, "repo.yaml"),
					"local": filepath.Join(dir, "local.yaml"),
				}
				writeFile(t, paths["org"], guiConfigBody(tc.orgGUI))
				lowBody := guiConfigBody(tc.lowGUI)
				writeFile(t, paths[layer.field], lowBody)
				for _, absent := range []string{"user", "repo", "local"} {
					if absent != layer.field {
						paths[absent] = filepath.Join(dir, "missing-"+absent+".yaml")
					}
				}
				opts := guiEffectiveOptions(dir, paths["org"], paths["user"], paths["repo"], paths["local"])

				ec := loadEffectiveForTest(context.Background(), opts)
				if len(ec.Errors) != 0 {
					t.Fatalf("LoadEffective errors: %v", ec.Errors)
				}
				if got := GUIReleaseCheckEnabled(organizationSettings(t, opts)); got != tc.want {
					t.Fatalf("effective release check = %v, want %v", got, tc.want)
				}

				guiWarn := guiWarningFor(t, ec.Warnings, layer.source)
				if tc.warning {
					if guiWarn == nil {
						t.Fatalf("missing %s warning for %s in %v", GUIOrganizationOnlyCode, layer.source, ec.Warnings)
					}
					if guiWarn.Code != GUIOrganizationOnlyCode {
						t.Fatalf("warning code = %q, want %q", guiWarn.Code, GUIOrganizationOnlyCode)
					}
					if guiWarn.Path == "" {
						t.Fatal("warning does not identify its source path")
					}
				} else if guiWarn != nil {
					t.Fatalf("unexpected warning %v", guiWarn)
				}

				after, err := os.ReadFile(paths[layer.field])
				if err != nil {
					t.Fatal(err)
				}
				if string(after) != lowBody {
					t.Fatalf("lower-layer root bytes changed: %q", after)
				}
				if tc.orgGUI == "" && ec.Merged != nil && ec.Merged.GUI != nil && ec.Merged.GUI.VersionCheck != nil {
					t.Fatal("merged config honored a lower-layer gui.version_check")
				}

				if tc.lowGUI == "" {
					return
				}
				settings, err := loadSettings(paths[layer.field], layer.kind)
				if err != nil {
					t.Fatalf("settings reader rejected gui: %v", err)
				}
				if settings.GUI != nil {
					t.Fatalf("lower-layer settings honored gui: %#v", settings.GUI)
				}
				if layer.source == RepoPersonal {
					if _, err := LoadLocalConfigFromPath(paths[layer.field]); err != nil {
						t.Fatalf("local reader rejected gui: %v", err)
					}
					return
				}
				if _, err := LoadConfigFromPath(paths[layer.field]); err != nil {
					t.Fatalf("direct reader rejected gui: %v", err)
				}
			})
		}
	}
}

// spec: config-layering/organization-setting-controls-release-availability-checks
// spec: config-layering/lower-layer-gui-setting-is-ignored-with-a-warning
func TestGUIMergeLayersKeepsOrganizationPolicy(t *testing.T) {
	guiValue := func(v bool) *GUIConfig { b := v; return &GUIConfig{VersionCheck: &b} }
	layer := func(source Source, gui *GUIConfig) Layer {
		return Layer{Source: source, Config: &Config{GUI: gui}}
	}
	for _, tc := range []struct {
		name   string
		layers []Layer
		want   *bool
	}{
		{
			name: "organization false wins over lower true",
			layers: []Layer{
				layer(Organization, guiValue(false)),
				layer(UserPersonal, guiValue(true)),
				layer(RepoCommitted, guiValue(true)),
				layer(RepoPersonal, guiValue(true)),
			},
			want: guiValue(false).VersionCheck,
		},
		{
			name: "organization true wins over lower false",
			layers: []Layer{
				layer(Organization, guiValue(true)),
				layer(UserPersonal, guiValue(false)),
				layer(RepoCommitted, guiValue(false)),
				layer(RepoPersonal, guiValue(false)),
			},
			want: guiValue(true).VersionCheck,
		},
		{
			name: "no organization ignores lower values",
			layers: []Layer{
				layer(UserPersonal, guiValue(false)),
				layer(RepoCommitted, guiValue(false)),
				layer(RepoPersonal, guiValue(false)),
			},
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantInputs := make([]bool, 0, len(tc.layers))
			for _, l := range tc.layers {
				if l.Config.GUI == nil || l.Config.GUI.VersionCheck == nil {
					t.Fatalf("test layers must set gui.version_check")
				}
				wantInputs = append(wantInputs, *l.Config.GUI.VersionCheck)
			}
			merged := MergeLayers(tc.layers)
			var got *bool
			if merged.GUI != nil {
				got = merged.GUI.VersionCheck
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("merged version_check = %v, want unset", *got)
				}
			} else if got == nil || *got != *tc.want {
				t.Fatalf("merged version_check = %v, want %v", got, *tc.want)
			}
			for i, l := range tc.layers {
				if l.Config.GUI == nil || l.Config.GUI.VersionCheck == nil || *l.Config.GUI.VersionCheck != wantInputs[i] {
					t.Fatalf("layer %d GUI input mutated", i)
				}
			}
		})
	}
}

// spec: config-layering/lower-layer-gui-setting-is-ignored-with-a-warning
func TestGUILowerLayerIncludedFragmentWarns(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	fragmentPath := filepath.Join(userDir, "fragment.yaml")
	userPath := filepath.Join(userDir, "config.yaml")
	orgPath := filepath.Join(dir, "org.yaml")

	fragmentBody := "gui:\n  version_check: false\n"
	userBody := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n"
	writeFile(t, fragmentPath, fragmentBody)
	writeFile(t, userPath, userBody)
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\ngui:\n  version_check: true\n")

	opts := guiEffectiveOptions(dir, orgPath, userPath,
		filepath.Join(dir, "missing-repo.yaml"), filepath.Join(dir, "missing-local.yaml"))
	ec := loadEffectiveForTest(context.Background(), opts)
	if len(ec.Errors) != 0 {
		t.Fatalf("LoadEffective errors: %v", ec.Errors)
	}
	if got := GUIReleaseCheckEnabled(organizationSettings(t, opts)); !got {
		t.Fatal("lower-layer included gui.version_check reached the effective policy")
	}
	if guiWarningFor(t, ec.Warnings, UserPersonal) == nil {
		t.Fatalf("missing %s warning for included fragment in %v", GUIOrganizationOnlyCode, ec.Warnings)
	}
	after, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != userBody {
		t.Fatalf("root bytes changed to duplicate the included entry: %q", after)
	}
	after, err = os.ReadFile(fragmentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != fragmentBody {
		t.Fatal("included fragment bytes changed")
	}
}

// spec: config-layering/organization-setting-controls-release-availability-checks
func TestGUIOrganizationIncludeComposition(t *testing.T) {
	dir := t.TempDir()
	orgDir := filepath.Join(dir, "org")
	orgPath := filepath.Join(orgDir, "config.yaml")
	writeFile(t, filepath.Join(orgDir, "policy.yaml"), "gui:\n  version_check: false\n")
	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\ninclude: policy.yaml\n")

	opts := guiEffectiveOptions(dir, orgPath,
		filepath.Join(dir, "missing-user.yaml"),
		filepath.Join(dir, "missing-repo.yaml"),
		filepath.Join(dir, "missing-local.yaml"))

	if got := GUIReleaseCheckEnabled(organizationSettings(t, opts)); got {
		t.Fatal("included organization gui.version_check: false was not preserved")
	}

	writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\ninclude: policy.yaml\ngui:\n  version_check: true\n")
	if got := GUIReleaseCheckEnabled(organizationSettings(t, opts)); !got {
		t.Fatal("explicit root gui.version_check: true did not override included false")
	}
}

func TestGUIMalformedDocumentsRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		gui  string
	}{
		{name: "scalar gui", gui: "gui: true\n"},
		{name: "sequence gui", gui: "gui: [version_check]\n"},
		{name: "nonboolean version_check", gui: "gui:\n  version_check: yes-please\n"},
		{name: "string version_check", gui: "gui:\n  version_check: \"false\"\n"},
		{name: "unknown nested key", gui: "gui:\n  other: true\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseV4Config([]byte(guiConfigBody(tc.gui)), "test.yaml", true); err == nil {
				t.Fatal("v4 decoder accepted malformed gui mapping")
			}
		})
	}

	cfg, err := ParseV4Config([]byte(guiConfigBody("gui:\n  version_check: false\n")), "test.yaml", true)
	if err != nil {
		t.Fatalf("v4 decoder rejected valid gui: %v", err)
	}
	if cfg.GUI == nil || cfg.GUI.VersionCheck == nil || *cfg.GUI.VersionCheck {
		t.Fatalf("v4 gui.version_check not decoded: %#v", cfg.GUI)
	}
}
