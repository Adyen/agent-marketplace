package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestEnsureMarketplacePinsAndSyncRollsBackWholeCollectionBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	original := []byte("apiVersion: agent-marketplace/v2\nmarketplaces: []\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	pins := []config.MarketplaceResourcePin{
		{SourceID: "src", Kind: "collection", Name: "bundle", Revision: digest, Digest: digest, Acquisition: config.MarketplaceAcquisitionMirror},
		{SourceID: "src", Kind: "skill", Name: "review", Revision: digest, Digest: digest, Acquisition: config.MarketplaceAcquisitionMirror},
	}
	wantErr := errors.New("leaf materialization failed")
	_, err := ensureMarketplacePinsAndSync(path, config.MarketplaceEntry{URL: "https://market.example", InstanceID: "mp", Alias: "public"}, pins, func() ([]error, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("collection batch was not rolled back: %q", got)
	}
}
