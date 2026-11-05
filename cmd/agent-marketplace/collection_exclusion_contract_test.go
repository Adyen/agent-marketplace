package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// collectionExclusionFixture is a repository whose committed v4 config selects
// collection/platform-bundle (members review and audit) plus an unrelated
// skill/keep from the same "hosted" source, and skill/neighbor from a separate
// "sibling" source.
type collectionExclusionFixture struct {
	tmp           string
	repoRoot      string
	configPath    string
	lockPath      string
	hostedOrigin  string
	siblingOrigin string
	// Lock records name each Git source without the bare repository suffix.
	hostedLockURL  string
	siblingLockURL string
}

func newCollectionExclusionFixture(t *testing.T, required bool) collectionExclusionFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	hostedPath, hostedWorking := createBareRepoWithWorkingClone(t, "collection-exclusion-hosted.git")
	writeSkill(t, hostedWorking, "review", "Review", nil)
	writeSkill(t, hostedWorking, "audit", "Audit", nil)
	writeSkill(t, hostedWorking, "keep", "Keep", nil)
	writeCollection(t, hostedWorking, "platform-bundle", `
apiVersion: agent-marketplace/v1
kind: Collection
name: platform-bundle
version: 1.0.0
skills:
  - name: review
  - name: audit
`)
	commitAndPushAll(t, hostedWorking, "seed hosted")

	siblingPath, siblingWorking := createBareRepoWithWorkingClone(t, "collection-exclusion-sibling.git")
	writeSkill(t, siblingWorking, "neighbor", "Neighbor", nil)
	commitAndPushAll(t, siblingWorking, "seed sibling")

	fx := collectionExclusionFixture{
		tmp:            tmp,
		repoRoot:       filepath.Join(t.TempDir(), "repo"),
		hostedOrigin:   "file://" + hostedPath,
		siblingOrigin:  "file://" + siblingPath,
		hostedLockURL:  "file://" + filepath.Join(filepath.Dir(hostedPath), "collection-exclusion-hosted"),
		siblingLockURL: "file://" + filepath.Join(filepath.Dir(siblingPath), "collection-exclusion-sibling"),
	}
	fx.configPath = filepath.Join(fx.repoRoot, ".agents", "config.yaml")
	fx.lockPath = filepath.Join(fx.repoRoot, ".agents", "lock.yaml")
	hostedGroup := "{include: [collection/platform-bundle, skill/keep]}"
	if required {
		hostedGroup = "{include: [collection/platform-bundle, skill/keep], required: true}"
	}
	writeConfig(t, fx.configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n"+
		"  hosted: {git: "+fx.hostedOrigin+", ref: main}\n"+
		"  sibling: {git: "+fx.siblingOrigin+", ref: main}\n"+
		"resources:\n"+
		"  hosted: "+hostedGroup+"\n"+
		"  sibling: [skill/neighbor]\n")
	withWorkingDir(t, fx.repoRoot)
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, output)
	}
	for _, name := range []string{"review", "audit", "keep", "neighbor"} {
		assertFileExists(t, fx.skillPath(name))
	}
	return fx
}

func (fx collectionExclusionFixture) skillPath(name string) string {
	return filepath.Join(fx.repoRoot, ".factory", "skills", name, "SKILL.md")
}

// normalizedConfig replaces per-fixture origins so two independently seeded
// fixtures can be compared byte for byte.
func (fx collectionExclusionFixture) normalizedConfig(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.ReplaceAll(string(raw), fx.hostedOrigin, "<hosted-origin>")
	return strings.ReplaceAll(normalized, fx.siblingOrigin, "<sibling-origin>")
}

func (fx collectionExclusionFixture) lockRegistry(t *testing.T, lockURL string) lockfile.GitRegistry {
	t.Helper()
	doc := readExactManifestLock(t, fx.lockPath)
	if doc.Scope != lockfile.ScopeRepo || len(doc.Registries) != 2 {
		t.Fatalf("lock scope=%v registries=%+v, want repo scope with hosted and sibling", doc.Scope, doc.Registries)
	}
	var found []lockfile.GitRegistry
	for _, registry := range doc.Registries {
		if registry.URL == lockURL {
			found = append(found, registry)
		}
	}
	if len(found) != 1 {
		t.Fatalf("lock registries for %s = %+v (all: %+v)", lockURL, found, doc.Registries)
	}
	return found[0]
}

type collectionExclusionRemoveVariant struct {
	name string
	args func(fx collectionExclusionFixture) []string
}

var collectionExclusionRemoveVariants = []collectionExclusionRemoveVariant{
	{name: "plain", args: func(collectionExclusionFixture) []string {
		return []string{"remove", "platform-bundle"}
	}},
	{name: "from source", args: func(fx collectionExclusionFixture) []string {
		return []string{"remove", "platform-bundle", "--from", fx.hostedOrigin}
	}},
}

func collectionExclusionLocators(requests []config.V4ResourceRequest) []string {
	out := []string{}
	for _, request := range requests {
		out = append(out, request.Locator)
	}
	slices.Sort(out)
	return out
}

func TestCollectionExclusionContractPlainAndFromAgree(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	finalConfigs := map[string]string{}
	for _, variant := range collectionExclusionRemoveVariants {
		t.Run(variant.name, func(t *testing.T) {
			fx := newCollectionExclusionFixture(t, false)
			siblingLockBefore := fx.lockRegistry(t, fx.siblingLockURL)
			neighborBefore, err := os.ReadFile(fx.skillPath("neighbor"))
			if err != nil {
				t.Fatal(err)
			}

			if output, err := executeCommand(variant.args(fx)...); err != nil {
				t.Fatalf("remove: %v\n%s", err, output)
			}

			raw, err := os.ReadFile(fx.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "registries:") || strings.Contains(string(raw), "collections:") {
				t.Fatalf("v4 config gained legacy selector keys:\n%s", raw)
			}
			cfg, err := config.LoadV4ConfigFromPath(fx.configPath)
			if err != nil {
				t.Fatalf("load v4 config: %v", err)
			}
			if len(cfg.Sources) != 2 || len(cfg.Groups) != 2 {
				t.Fatalf("sources/groups = %#v %#v\n%s", cfg.Sources, cfg.Groups, raw)
			}
			hosted, ok := cfg.Groups["hosted"]
			if !ok {
				t.Fatalf("hosted group missing:\n%s", raw)
			}
			if got := collectionExclusionLocators(hosted.Include); !slices.Equal(got, []string{"skill/keep"}) {
				t.Fatalf("hosted include = %v, want [skill/keep]\n%s", got, raw)
			}
			if !slices.Equal(hosted.Exclude, []string{"collection/platform-bundle"}) {
				t.Fatalf("hosted exclude = %v, want [collection/platform-bundle]\n%s", hosted.Exclude, raw)
			}
			sibling, ok := cfg.Groups["sibling"]
			if !ok {
				t.Fatalf("sibling group missing:\n%s", raw)
			}
			if got := collectionExclusionLocators(sibling.Include); !slices.Equal(got, []string{"skill/neighbor"}) || len(sibling.Exclude) != 0 {
				t.Fatalf("sibling group include=%v exclude=%v, want include [skill/neighbor] and no exclusion\n%s", got, sibling.Exclude, raw)
			}

			for _, member := range []string{"review", "audit"} {
				if _, err := os.Lstat(filepath.Join(fx.repoRoot, ".factory", "skills", member)); !os.IsNotExist(err) {
					t.Fatalf("collection member %s still materialized: %v", member, err)
				}
			}
			assertFileExists(t, fx.skillPath("keep"))
			neighborAfter, err := os.ReadFile(fx.skillPath("neighbor"))
			if err != nil || !bytes.Equal(neighborBefore, neighborAfter) {
				t.Fatalf("sibling output changed: %v\n%s", err, neighborAfter)
			}
			if siblingLockAfter := fx.lockRegistry(t, fx.siblingLockURL); !reflect.DeepEqual(siblingLockBefore, siblingLockAfter) {
				t.Fatalf("sibling lock entry changed:\nbefore %+v\nafter  %+v", siblingLockBefore, siblingLockAfter)
			}
			// skill/keep still selects from hosted, so its lock record remains.
			fx.lockRegistry(t, fx.hostedLockURL)
			if output, err := executeCommand("sync", "--check"); err != nil {
				t.Fatalf("sync --check after remove: %v\n%s", err, output)
			}
			assertNoStagingResidue(t, fx.tmp)
			finalConfigs[variant.name] = fx.normalizedConfig(t)
		})
	}
	plain, from := finalConfigs["plain"], finalConfigs["from source"]
	if plain == "" || from == "" {
		t.Fatalf("missing variant result: plain=%q from=%q", plain, from)
	}
	if plain != from {
		t.Fatalf("plain and --from removal diverged:\n--- plain\n%s\n--- from source\n%s", plain, from)
	}
}

func TestCollectionExclusionContractRequiredRefusalPreservesState(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, variant := range collectionExclusionRemoveVariants {
		t.Run(variant.name, func(t *testing.T) {
			fx := newCollectionExclusionFixture(t, true)
			preserved := map[string][]byte{}
			for _, path := range []string{fx.configPath, fx.lockPath, fx.skillPath("review"), fx.skillPath("audit"), fx.skillPath("keep"), fx.skillPath("neighbor")} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				preserved[path] = data
			}

			output, err := executeCommand(variant.args(fx)...)
			if err == nil {
				t.Fatalf("remove of a required collection succeeded:\n%s", output)
			}
			if !strings.Contains(err.Error(), "required") {
				t.Fatalf("remove error = %v, want required-policy refusal\n%s", err, output)
			}
			for path, before := range preserved {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Errorf("refused remove changed %s: %v", path, err)
				}
			}
			assertNoStagingResidue(t, fx.tmp)
		})
	}
}
