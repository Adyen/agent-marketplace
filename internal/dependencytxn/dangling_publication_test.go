package dependencytxn

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotPreservesDanglingConfigCondition(t *testing.T) {
	for _, name := range []string{"config.yaml", "config.local.yaml"} {
		t.Run(name, func(t *testing.T) {
			_, agents, _ := userLayer(t)
			destination := filepath.Join(agents, name)
			if err := os.Symlink("missing-target", destination); err != nil {
				t.Fatal(err)
			}
			data, generation, mode, err := ReadSnapshot(destination)
			if !errors.Is(err, ErrUnsafePath) || !strings.Contains(err.Error(), "symlink to a missing target") || data != nil || generation.Exists || mode != 0 {
				t.Fatalf("dangling config snapshot lost its public condition: %v", err)
			}
		})
	}
}
