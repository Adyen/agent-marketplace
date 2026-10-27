package harness

import (
	"fmt"
	"os"
	"path/filepath"
)

func claudeMachineRoot(home string) (string, error) {
	if custom := os.Getenv("CLAUDE_CONFIG_DIR"); custom != "" {
		if !filepath.IsAbs(custom) {
			return "", fmt.Errorf("CLAUDE_CONFIG_DIR must be absolute")
		}
		return filepath.Clean(custom), nil
	}
	return filepath.Join(home, ".claude"), nil
}
