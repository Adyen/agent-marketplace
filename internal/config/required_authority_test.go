package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `required: true` immunizes a registry against every exclude, so it is
// organization policy and nothing else may claim it. These tests pin the rule
// at every door into the loader — strict v3, the v2 decoder, the effective-config
// layers, and the migration that rewrites v2 into v3.

const requiredAuthorityURL = "git@example.com:org/security.git"

func requiredEntryDoc(apiVersion, value string) []byte {
	head := ""
	if apiVersion != "" {
		head = "apiVersion: " + apiVersion + "\n"
	}
	return []byte(head + `registries:
  - url: ` + requiredAuthorityURL + `
    ref: main
    required: ` + value + `
`)
}

func TestRequiredIsRejectedOutsideOrganizationLayer(t *testing.T) {
	for _, kind := range []layerKind{userLayer, repoLayer, localLayer} {
		t.Run("v3/"+kind.name(), func(t *testing.T) {
			_, errs := loadV3Config(requiredEntryDoc(APIVersionV3, "true"), kind, false)
			if len(errs) == 0 {
				t.Fatalf("expected %s config to be refused", kind.name())
			}
			if !strings.Contains(errorsJoin(errs), "only organization policy may set required") {
				t.Fatalf("expected an authority error, got %v", errs)
			}
		})

		t.Run("v2/"+kind.name(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			mustWriteFile(t, path, requiredEntryDoc(APIVersionV2, "true"))
			_, err := loadV3OrV2Config(path, kind, false)
			if err == nil {
				t.Fatalf("expected %s config to be refused", kind.name())
			}
			if !strings.Contains(err.Error(), "only allowed in organization configuration") {
				t.Fatalf("expected an authority error, got %v", err)
			}
		})
	}

	t.Run("organization", func(t *testing.T) {
		cfg, errs := loadV3Config(requiredEntryDoc(APIVersionV3, "true"), organizationLayer, false)
		if len(errs) != 0 {
			t.Fatalf("organization policy must keep required, got %v", errs)
		}
		if !cfg.Registries[0].Required {
			t.Fatal("expected the organization entry to stay required")
		}
	})
}

// A value that is not a boolean cannot be read as a disclaimer: an entry
// carrying one must not reach the decoder as a trusted flag.
func TestRequiredWithNonBooleanValueIsRejectedOutsideOrganizationLayer(t *testing.T) {
	for _, value := range []string{"yes", "\"true\"", "[]"} {
		_, errs := loadV3Config(requiredEntryDoc(APIVersionV3, value), repoLayer, false)
		if len(errs) == 0 {
			t.Fatalf("expected required: %s to be refused in a repository config", value)
		}
	}
}

// required: false claims nothing. Refusing it would make the strict v3 loader
// reject documents the v2 loader accepts, so both must agree.
func TestRequiredFalseIsAcceptedInEveryLayer(t *testing.T) {
	for _, kind := range []layerKind{organizationLayer, userLayer, repoLayer, localLayer} {
		t.Run("v3/"+kind.name(), func(t *testing.T) {
			cfg, errs := loadV3Config(requiredEntryDoc(APIVersionV3, "false"), kind, false)
			if len(errs) != 0 {
				t.Fatalf("expected required: false to load in %s config, got %v", kind.name(), errs)
			}
			if cfg.Registries[0].Required {
				t.Fatal("required: false must not grant authority")
			}
		})

		t.Run("v2/"+kind.name(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			mustWriteFile(t, path, requiredEntryDoc(APIVersionV2, "false"))
			cfg, err := loadV3OrV2Config(path, kind, false)
			if err != nil {
				t.Fatalf("expected required: false to load in %s config, got %v", kind.name(), err)
			}
			if cfg.Registries[0].Required {
				t.Fatal("required: false must not grant authority")
			}
		})
	}
}

func TestEffectiveConfigRefusesRequiredFromRepoAndLocalLayers(t *testing.T) {
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "repo")
	repoConfig := filepath.Join(repoRoot, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(repoConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, repoConfig, requiredEntryDoc(APIVersionV3, "true"))

	ec := LoadEffective(t.Context(), LoadEffectiveOptions{
		RepoRoot:         repoRoot,
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       filepath.Join(dir, "missing-user.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})
	if len(ec.Errors) == 0 {
		t.Fatal("expected a repository config claiming required to be refused")
	}
	if !strings.Contains(errorsJoin(ec.Errors), "required") {
		t.Fatalf("expected the error to name required, got %v", ec.Errors)
	}
}

// Migration cannot keep a required claim (the result would fail its own
// validation) and must not drop it silently either, so it reports every removal.
func TestMigrationDropsRequiredAndReportsIt(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.yaml")
	repoPath := filepath.Join(dir, "repo.yaml")
	mustWriteFile(t, userPath, []byte(`apiVersion: agent-marketplace/v2
harnesses: [factory]
registries:
  # keep this comment
  - url: `+requiredAuthorityURL+`
    ref: main
    required: true
  - url: git@example.com:org/opt-in.git
    ref: main
    required: false
    default: true
`))
	mustWriteFile(t, repoPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:team/repo.git
    ref: main
    required: true
`))

	result, err := MigrateConfigFiles(MigratePaths{User: userPath, Repo: repoPath}, nil)
	if err != nil {
		t.Fatalf("migration must not fail on a required claim it can drop: %v", err)
	}
	if len(result.DroppedRequired) != 2 {
		t.Fatalf("expected both required claims to be reported, got %#v", result.DroppedRequired)
	}
	byURL := map[string]DroppedRequiredRegistry{}
	for _, dropped := range result.DroppedRequired {
		byURL[dropped.URL] = dropped
	}
	if got := byURL[requiredAuthorityURL]; got.Layer != userLayer.name() || got.Path != userPath {
		t.Fatalf("expected the user layer removal to be attributed, got %#v", got)
	}
	if got := byURL["git@example.com:team/repo.git"]; got.Layer != repoLayer.name() || got.Path != repoPath {
		t.Fatalf("expected the repository layer removal to be attributed, got %#v", got)
	}

	migratedUser := readFile(t, userPath)
	if strings.Contains(migratedUser, "required: true") {
		t.Fatalf("migrated user config still claims required authority:\n%s", migratedUser)
	}
	for _, want := range []string{"keep this comment", "required: false", "default: true"} {
		if !strings.Contains(migratedUser, want) {
			t.Fatalf("migration must preserve %q:\n%s", want, migratedUser)
		}
	}

	// The migrated files must load under the schema they now declare.
	if _, err := loadV3OrV2Config(userPath, userLayer, true); err != nil {
		t.Fatalf("migrated user config does not load: %v", err)
	}
	if _, err := loadV3OrV2Config(repoPath, repoLayer, false); err != nil {
		t.Fatalf("migrated repo config does not load: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
