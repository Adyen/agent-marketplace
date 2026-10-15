package pipeline

import (
	"context"
	"errors"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// ValidateConsumerIntent checks every required authored layer and include
// without acquiring dependencies. Callers handle pending transactions first.
func ValidateConsumerIntent(ctx context.Context, repoRoot, localConfigDir string) error {
	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{RepoRoot: repoRoot, LocalConfigDir: localConfigDir})
	return errors.Join(intent.Errors...)
}
