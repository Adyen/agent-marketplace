package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	migrationDigestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	migrationDigestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestStageMarketplaceConfigMigrationExtractsDirectRequirementsAndSeeds(t *testing.T) {
	input := []byte(`# document
apiVersion: agent-marketplace/v2
other: 'keep-style' # keep comment
marketplaces:
  - url: https://market.example # endpoint
    instance_id: market
    alias: public
    resources:
      # collection root
      - {source_id: collections, kind: collection, name: baseline, revision: ` + migrationDigestA + `, digest: ` + migrationDigestA + `}
      # generated leaf
      - source_id: generated
        kind: skill
        name: generated
        revision: ` + migrationDigestA + `
        digest: ` + migrationDigestA + `
        selected_by:
          - source_id: collections
            name: baseline
            revision: ` + migrationDigestA + `
      # direct and collection-selected leaf
      - source_id: direct
        kind: skill
        name: review
        installed_name: 'review-copy'
        revision: ` + migrationDigestB + `
        digest: ` + migrationDigestB + `
        direct: true
        selected_by:
          - source_id: collections
            name: baseline
            revision: ` + migrationDigestA + `
      - source_id: already
        kind: command
        name: canonical
        revision: ` + migrationDigestB)

	got, err := StageMarketplaceConfigMigration(input)
	if err != nil {
		t.Fatal(err)
	}
	if !got.MigrationNeeded {
		t.Fatal("legacy exact resources did not request migration")
	}
	if len(got.LegacySeeds) != 1 {
		t.Fatalf("legacy seed groups = %#v", got.LegacySeeds)
	}
	seed := got.LegacySeeds[0]
	if seed.URL != "https://market.example" || seed.InstanceID != "market" || seed.Alias != "public" {
		t.Fatalf("legacy seed endpoint = %#v", seed)
	}
	if names := []string{seed.Resources[0].Name, seed.Resources[1].Name, seed.Resources[2].Name}; !reflect.DeepEqual(names, []string{"baseline", "generated", "review"}) {
		t.Fatalf("legacy seed resources = %v", names)
	}

	want := []byte(`# document
apiVersion: agent-marketplace/v2
other: 'keep-style' # keep comment
marketplaces:
  - url: https://market.example # endpoint
    instance_id: market
    alias: public
    resources:
      # collection root
      - {source_id: collections, kind: collection, name: baseline}
      # direct and collection-selected leaf
      - source_id: direct
        kind: skill
        name: review
        installed_name: 'review-copy'
      - source_id: already
        kind: command
        name: canonical
        revision: ` + migrationDigestB)
	if !bytes.Equal(got.SimplifiedConfig, want) {
		t.Fatalf("simplified config mismatch\nwant:\n%s\n\ngot:\n%s", want, got.SimplifiedConfig)
	}
}

func TestStageMarketplaceConfigMigrationCanonicalInputIsByteExactNoOp(t *testing.T) {
	input := []byte("marketplaces: [{url: 'https://market.example', instance_id: market, resources: [{source_id: source, kind: skill, name: review, installed_name: 'local', revision: " + migrationDigestA + "}]}]\n")
	got, err := StageMarketplaceConfigMigration(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.MigrationNeeded || len(got.LegacySeeds) != 0 || !bytes.Equal(got.SimplifiedConfig, input) {
		t.Fatalf("canonical staging changed input: %#v\n%s", got, got.SimplifiedConfig)
	}
}

func TestStageMarketplaceConfigMigrationRejectsPartialHybridWithoutSideEffects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	input := []byte("marketplaces:\n  - url: https://market.example\n    instance_id: market\n    resources:\n      - source_id: source\n        kind: skill\n        name: review\n        digest: " + migrationDigestA + "\n")
	if err := os.WriteFile(path, input, 0o640); err != nil {
		t.Fatal(err)
	}
	beforeEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := StageMarketplaceConfigMigrationFromPath(path)
	if err == nil || !errors.Is(err, ErrMarketplaceMigrationInput) {
		t.Fatalf("partial hybrid result = %#v, error = %v", got, err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, input) {
		t.Fatalf("failed staging changed source bytes\nwant: %q\ngot:  %q", input, after)
	}
	afterEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterEntries) != len(beforeEntries) || afterEntries[0].Name() != beforeEntries[0].Name() {
		t.Fatalf("failed staging left filesystem residue: before=%v after=%v", beforeEntries, afterEntries)
	}
}

func TestV3MarketplaceRequirementShape(t *testing.T) {
	marshaled, err := yaml.Marshal(MarketplaceResourcePin{SourceID: "source", Kind: "skill", Name: "review", InstalledName: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "source_id: source\nkind: skill\nname: review\ninstalled_name: local\n"; string(marshaled) != want {
		t.Fatalf("canonical requirement marshal\nwant: %q\ngot:  %q", want, marshaled)
	}

	canonical := []byte("apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: https://market.example\n    instance_id: market\n    resources:\n      - source_id: source\n        kind: skill\n        name: review\n        revision: " + migrationDigestA + "\n")
	if _, errs := loadV3Config(canonical, repoLayer, false); len(errs) != 0 {
		t.Fatalf("canonical requirement rejected: %v", errs)
	}

	partialHybrid := bytes.Replace(canonical, []byte("        revision: "+migrationDigestA), []byte("        digest: "+migrationDigestA), 1)
	if _, errs := loadV3Config(partialHybrid, repoLayer, false); len(errs) == 0 {
		t.Fatal("strict config loader accepted a partial legacy hybrid")
	}

	missingIdentity := bytes.Replace(canonical, []byte("        name: review\n"), nil, 1)
	if _, errs := loadV3Config(missingIdentity, repoLayer, false); len(errs) == 0 {
		t.Fatal("strict config loader accepted a requirement without name")
	}
}
