package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestEditV4ResourceSelectionInvalidDocumentIsHandledAndUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("apiVersion: agent-marketplace/v4\nunknown_field: true\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	handled, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"}, ir.KindSkill, "alpha", V4SelectionEnable)
	if !handled || err == nil {
		t.Fatalf("handled %v err %v", handled, err)
	}
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.unknown-field" {
		t.Fatalf("error = %T %v", err, err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(data) {
		t.Fatalf("invalid document changed: %s", got)
	}
}
