package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func mutateIncludeSkill(m *LayerMutation) error {
	binding, err := m.EnsureRegistry(config.RegistryEntry{URL: "https://example.com/registry.git", Ref: "main"})
	if err != nil {
		return err
	}
	return m.Apply(binding, config.SelectionChange{Operation: config.SelectionInclude, Locators: []config.ResourceLocator{{Kind: ir.KindSkill, Name: "one"}}})
}

func isolateConsumerPipeline(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	config.SetTestOrganizationConfigPath(t, filepath.Join(home, "missing-org.yaml"))
	return home
}

func assertConsumerFormatError(t *testing.T, err error, code, path string) {
	t.Helper()
	var diagnostic *config.V4ValidationError
	if !errors.As(err, &diagnostic) || diagnostic.Code != code || diagnostic.Path != path || !strings.Contains(diagnostic.Msg, "apiVersion") {
		t.Fatalf("error = %v, want %s at %s apiVersion", err, code, path)
	}
}

// spec: consumer-config-format/mutations-refuse-before-new-writes-or-prunes
func TestDirectLayerEditorRefusesUnsupportedRoots(t *testing.T) {
	for _, body := range []string{"# versionless\n", "apiVersion: agent-marketplace/v1\n", "apiVersion: agent-marketplace/v2\n", "apiVersion: agent-marketplace/v3\n", "apiVersion: null\n"} {
		t.Run(strings.TrimSpace(body), func(t *testing.T) {
			isolateConsumerPipeline(t)
			repo := t.TempDir()
			path := config.ProjectConfigPath(repo)
			writeFileMode(t, path, body, 0o600)
			frozen := time.Unix(1700000000, 0)
			if err := os.Chtimes(path, frozen, frozen); err != nil {
				t.Fatal(err)
			}
			called := false
			err := EditLayerAtPath(config.LayerTarget{Source: config.RepoCommitted, StagedPath: path, DeclaringPath: path}, func(m *LayerMutation) error {
				called = true
				return mutateIncludeSkill(m)
			})
			code := "v4.api-version"
			if !strings.Contains(body, "apiVersion") {
				code = "v4.api-version-required"
			}
			assertConsumerFormatError(t, err, code, path)
			data, err := os.ReadFile(path)
			if err != nil || string(data) != body || called {
				t.Fatalf("refusal changed root or ran callback: %q, %v, called=%t", data, err, called)
			}
			info, err := os.Stat(path)
			if err != nil || !info.ModTime().Equal(frozen) || info.Mode().Perm() != 0o600 {
				t.Fatalf("refusal changed metadata: %v, %v", info, err)
			}
		})
	}
	// The editor control really adds the selection, not a no-op callback.
	isolateConsumerPipeline(t)
	repo := t.TempDir()
	path := config.ProjectConfigPath(repo)
	writeFileMode(t, path, "apiVersion: agent-marketplace/v4\n", 0o600)
	if err := EditLayerAtPath(config.LayerTarget{Source: config.RepoCommitted, StagedPath: path, DeclaringPath: path}, mutateIncludeSkill); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "skill/one") {
		t.Fatalf("v4 control did not edit selection: %s, %v", data, err)
	}
}

// spec: consumer-config-format/refused-layers-prevent-missing-root-creation
func TestLayerMutationsRefuseBeforeMissingRootCreation(t *testing.T) {
	for _, direct := range []bool{true, false} {
		t.Run(map[bool]string{true: "direct", false: "staged"}[direct], func(t *testing.T) {
			home := isolateConsumerPipeline(t)
			repo := t.TempDir()
			invalid := filepath.Join(home, ".agents", "config.yaml")
			writeFileMode(t, invalid, "apiVersion: agent-marketplace/v3\n", 0o600)
			target := config.ProjectConfigPath(repo)
			called := false
			mutate := func(m *LayerMutation) error { called = true; return mutateIncludeSkill(m) }
			var err error
			if direct {
				err = EditLayerAtPath(config.LayerTarget{Source: config.RepoCommitted, StagedPath: target, DeclaringPath: target}, mutate)
			} else {
				err = MutateLayer(context.Background(), LayerMutationTarget{RepoRoot: repo, ConfigPath: target, Source: config.RepoCommitted, Reconcile: func(context.Context, string, string, bool, []StagedConfigEdit) (DependencyLockView, error) {
					t.Fatal("refused intent reached reconciliation")
					return DependencyLockView{}, nil
				}}, mutate)
			}
			assertConsumerFormatError(t, err, "v4.api-version", invalid)
			if called {
				t.Fatal("refused intent reached mutation")
			}
			if _, err := os.Stat(filepath.Dir(target)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refusal created target config/lock directory: %v", err)
			}
		})
	}
}

// spec: consumer-config-format/mutations-refuse-before-new-writes-or-prunes
func TestStagedMutationRevalidatesFormatBeforeReconcile(t *testing.T) {
	isolateConsumerPipeline(t)
	t.Setenv("TMPDIR", t.TempDir())
	repo := canonicalTempDir(t)
	path := config.ProjectConfigPath(repo)
	original := "apiVersion: agent-marketplace/v4\n"
	writeFileMode(t, path, original, 0o600)
	called := false
	err := MutateConfigWithDependencies(context.Background(), repo, repo, func(mapper ConfigPathMapper) error {
		shadow, err := mapper(path)
		if err != nil {
			return err
		}
		return os.WriteFile(shadow, []byte("apiVersion: agent-marketplace/v2\n"), 0o600)
	}, func(context.Context, string, string, bool, []StagedConfigEdit) (DependencyLockView, error) {
		called = true
		return DependencyLockView{}, nil
	})
	assertConsumerFormatError(t, err, "v4.api-version", path)
	if data, err := os.ReadFile(path); err != nil || string(data) != original || called {
		t.Fatalf("invalid staged output escaped: %q, %v, reconcile=%t", data, err, called)
	}
	assertNoStagingResidue(t)
	// A supported candidate actually reaches the same publication callback.
	candidate := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
	err = MutateConfigWithDependencies(context.Background(), repo, repo, func(mapper ConfigPathMapper) error {
		shadow, err := mapper(path)
		if err != nil {
			return err
		}
		return os.WriteFile(shadow, []byte(candidate), 0o600)
	}, commitStagedEdits(filepath.Join(repo, "state")))
	if data, readErr := os.ReadFile(path); err != nil || readErr != nil || string(data) != candidate {
		t.Fatalf("supported staged publication control: %q, %v, %v", data, err, readErr)
	}
	assertNoStagingResidue(t)
}

func TestV4MarketplaceEndpointNoopPreservesMetadata(t *testing.T) {
	home := isolateConsumerPipeline(t)
	repo := t.TempDir()
	path := filepath.Join(home, ".agents", "config.yaml")
	fake := newLayerMarketplaceServer(t)
	writeFileMode(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  public:\n    marketplace: "+fake.URL+"\n", 0o600)
	entry := config.MarketplaceEntry{URL: fake.URL, InstanceID: fake.instanceID}
	// First call creates exact endpoint evidence; second is a genuine no-op.
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, path, config.UserPersonal, func(m *MarketplaceLayerMutation) error { return m.EnsureEndpoint(entry) }, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	frozen := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, frozen, frozen); err != nil {
		t.Fatal(err)
	}
	if _, err := MutateMarketplaceLayer(context.Background(), repo, repo, path, config.UserPersonal, func(m *MarketplaceLayerMutation) error { return m.EnsureEndpoint(entry) }, nil); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("no-op changed config: %q, %v", after, err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(frozen) || info.Mode().Perm() != 0o600 {
		t.Fatalf("no-op changed metadata: %v, %v", info, err)
	}
}
