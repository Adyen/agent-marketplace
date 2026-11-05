package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type marketplaceEndpointFixture struct {
	repo, source, home, path, lockPath string
	fake                               *layerMarketplaceServer
	digest                             string
}

func newMarketplaceEndpointFixture(t *testing.T, localHealthy bool) marketplaceEndpointFixture {
	t.Helper()
	f := marketplaceEndpointFixture{home: isolateConsumerPipeline(t), repo: t.TempDir(), source: t.TempDir()}
	t.Chdir(f.repo)
	writeSkill(t, f.source, "keep", "Keep selected", nil)
	if localHealthy {
		writeSkill(t, f.source, "team", "Local team", nil)
	}
	f.fake = newLayerMarketplaceServer(t)
	registry := f.fake.addRegistry(t, "source")
	f.fake.addSkill(t, registry, "team")
	f.path = config.ProjectConfigPath(f.repo)
	f.lockPath = filepath.Join(f.repo, ".agents", "lock.yaml")
	writeFile(t, f.path, fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  local: {directory: %q, source_harness: factory}\nresources:\n  local: [skill/team, skill/keep]\n", f.source))
	f.digest = f.includeMarketplacePin(t, f.path, config.RepoCommitted)
	return f
}

func (f marketplaceEndpointFixture) includeMarketplacePin(t *testing.T, path string, source config.Source) string {
	t.Helper()
	pins := layerPreparePins(t, f.fake, "skill", "team")
	if _, err := MutateMarketplaceLayer(context.Background(), f.repo, f.repo, path, source, func(m *MarketplaceLayerMutation) error {
		return m.IncludePins(config.MarketplaceEntry{URL: f.fake.URL, InstanceID: f.fake.instanceID}, pins)
	}, nil); err != nil {
		t.Fatal(err)
	}
	return pins[0].Digest
}

func (f marketplaceEndpointFixture) read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func marketplaceTeamPinned(t *testing.T, path string) bool {
	t.Helper()
	cfg, err := config.LoadRootConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range cfg.Marketplaces {
		if slices.ContainsFunc(entry.Resources, func(pin config.MarketplaceResourcePin) bool { return pin.Kind == "skill" && pin.Name == "team" }) {
			return true
		}
	}
	return false
}

func localSkillSelections(t *testing.T, path string) []string {
	t.Helper()
	cfg, err := config.LoadRootConfigFromPath(path)
	if err != nil || len(cfg.Registries) != 1 {
		t.Fatalf("load local selections: %#v / %v", cfg, err)
	}
	return cfg.Registries[0].Skills.Include
}

var suggestedRemovalPattern = regexp.MustCompile(`--scope (\w+) --from ("(?:[^"\\]|\\.)*")((?: --(?:ref|path) "(?:[^"\\]|\\.)*")*)`)

// suggestedRemoval parses the remediation line naming fromURL exactly as a
// user would copy it from the refusal.
func suggestedRemoval(t *testing.T, err error, repo, name, fromURL string) RemoveOptions {
	t.Helper()
	if err == nil {
		t.Fatal("expected an ambiguity refusal")
	}
	for _, match := range suggestedRemovalPattern.FindAllStringSubmatch(err.Error(), -1) {
		from, unquoteErr := strconv.Unquote(match[2])
		if unquoteErr != nil || from != fromURL {
			continue
		}
		if match[3] != "" {
			t.Fatalf("suggestion for %q carries qualifiers %q:\n%v", fromURL, match[3], err)
		}
		scope, parseErr := ParseScope(match[1])
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		return RemoveOptions{RepoRoot: repo, ResourceName: name, Scope: scope, ScopeSpecified: true, FromURL: from}
	}
	t.Fatalf("no executable suggestion for %q in:\n%v", fromURL, err)
	return RemoveOptions{}
}

// spec: resource-removal/remove-a-marketplace-pin-by-its-endpoint
func TestRemoveMarketplaceSuggestedEndpoint(t *testing.T) {
	for _, localHealthy := range []bool{false, true} {
		for _, target := range []string{"marketplace", "local"} {
			t.Run(fmt.Sprintf("healthy=%t/%s", localHealthy, target), func(t *testing.T) {
				f := newMarketplaceEndpointFixture(t, localHealthy)
				configBefore, lockBefore := f.read(t, f.path), f.read(t, f.lockPath)
				_, err := Remove(context.Background(), RemoveOptions{RepoRoot: f.repo, ResourceName: "team", Scope: ScopeRepo})
				if !bytes.Equal(f.read(t, f.path), configBefore) || !bytes.Equal(f.read(t, f.lockPath), lockBefore) {
					t.Fatal("refusal changed configuration or acquisition lock")
				}
				identity := f.fake.URL
				if target == "local" {
					identity = f.source
				}
				if _, err := Remove(context.Background(), suggestedRemoval(t, err, f.repo, "team", identity)); err != nil {
					t.Fatalf("suggested removal failed: %v", err)
				}
				wantLocal, wantPinned := []string{"keep", "team"}, false
				if target == "local" {
					wantLocal, wantPinned = []string{"keep"}, true
				}
				if got := localSkillSelections(t, f.path); !slices.Equal(got, wantLocal) {
					t.Fatalf("local selections = %#v, want %#v", got, wantLocal)
				}
				if pinned, lockPinned := marketplaceTeamPinned(t, f.path), bytes.Contains(f.read(t, f.lockPath), []byte(f.digest)); pinned != wantPinned || lockPinned != wantPinned {
					t.Fatalf("Marketplace pin in config=%t lock=%t, want %t", pinned, lockPinned, wantPinned)
				}
			})
		}
	}
}

// spec: resource-removal/remove-a-marketplace-pin-by-its-endpoint
func TestRemoveMarketplaceEndpointScope(t *testing.T) {
	for _, scope := range []string{"default", "user", "user-only-default"} {
		t.Run(scope, func(t *testing.T) {
			f := newMarketplaceEndpointFixture(t, false)
			userPath := filepath.Join(f.home, ".agents", "config.yaml")
			writeFile(t, userPath, "apiVersion: agent-marketplace/v4\n")
			f.includeMarketplacePin(t, userPath, config.UserPersonal)
			if scope == "user-only-default" {
				if _, err := Remove(context.Background(), RemoveOptions{RepoRoot: f.repo, ResourceName: "team", Scope: ScopeRepo, ScopeSpecified: true, FromURL: f.fake.URL}); err != nil {
					t.Fatal(err)
				}
			}
			repoBefore, userBefore := f.read(t, f.path), f.read(t, userPath)
			opts := RemoveOptions{RepoRoot: f.repo, ResourceName: "team", Scope: ScopeRepo, FromURL: f.fake.URL}
			if scope == "user" {
				opts.Scope, opts.ScopeSpecified = ScopeUser, true
			}
			_, err := Remove(context.Background(), opts)
			if scope == "user-only-default" {
				if err == nil || !bytes.Equal(f.read(t, f.path), repoBefore) || !bytes.Equal(f.read(t, userPath), userBefore) {
					t.Fatalf("repository-default removal edited a user pin: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if marketplaceTeamPinned(t, f.path) != (scope == "user") || marketplaceTeamPinned(t, userPath) != (scope != "user") {
				t.Fatalf("wrong scope removed: repo=%t user=%t", marketplaceTeamPinned(t, f.path), marketplaceTeamPinned(t, userPath))
			}
			otherPath, otherBefore := userPath, userBefore
			if scope == "user" {
				otherPath, otherBefore = f.path, repoBefore
			}
			if !bytes.Equal(f.read(t, otherPath), otherBefore) {
				t.Fatal("other scope changed")
			}
		})
	}
}

// spec: resource-removal/refuse-ambiguous-or-unsupported-marketplace-endpoint-removal
func TestRemoveMarketplaceEndpointRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, resource, ref, path string
		kind, alias               bool
		code                      error
	}{
		{name: "path", resource: "team", path: "source-id", code: ErrMarketplaceRemoveUnsupportedQualifier},
		{name: "ref", resource: "team", ref: "main", code: ErrMarketplaceRemoveUnsupportedQualifier},
		{name: "missing", resource: "absent", code: ErrMarketplaceRemoveNotSelected},
		{name: "kind", resource: "team", kind: true, code: ErrMarketplaceRemoveNotSelected},
		{name: "registry-alias", resource: "team", alias: true, code: ErrMarketplaceRemoveAmbiguousSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMarketplaceEndpointFixture(t, false)
			if tc.alias {
				body := strings.Replace(string(f.read(t, f.path)), "sources:\n", fmt.Sprintf("sources:\n  alias: {git: %q, ref: main}\n", f.fake.URL), 1)
				writeFile(t, f.path, body)
			}
			configBefore, lockBefore := f.read(t, f.path), f.read(t, f.lockPath)
			opts := RemoveOptions{RepoRoot: f.repo, ResourceName: tc.resource, Scope: ScopeRepo, FromURL: f.fake.URL, FromRef: tc.ref, FromPath: tc.path}
			if tc.kind {
				opts.Kind, opts.HasKind = ir.KindCommand, true
			}
			_, err := Remove(context.Background(), opts)
			if !errors.Is(err, tc.code) || !strings.HasPrefix(err.Error(), tc.code.Error()+": ") {
				t.Fatalf("expected %v refusal, got %v", tc.code, err)
			}
			if !bytes.Equal(f.read(t, f.path), configBefore) || !bytes.Equal(f.read(t, f.lockPath), lockBefore) {
				t.Fatal("refusal changed configuration or acquisition lock")
			}
			if !marketplaceTeamPinned(t, f.path) {
				t.Fatal("non-vacuity: fixture lost its Marketplace pin")
			}
		})
	}
}
