package harness

import (
	"fmt"
	"os"
	"path/filepath"
)

func claudeMachineRoot(home string) (string, error) {
	if custom := os.Getenv("CLAUDE_CONFIG_DIR"); custom != "" {
		if ClaudeConfigDirRelative(custom) {
			return "", fmt.Errorf("CLAUDE_CONFIG_DIR must be absolute")
		}
		return filepath.Clean(custom), nil
	}
	return filepath.Join(home, ".claude"), nil
}

// ClaudeConfigDirRelative identifies an invalid nonempty override value.
func ClaudeConfigDirRelative(value string) bool {
	return value != "" && !filepath.IsAbs(value)
}
