package bundle

import "github.com/Adyen/agent-marketplace/pkg/harness-compilation/internal/yamlfm"

// MaxFrontmatterBytes is the YAML frontmatter parse ceiling, exposed for
// consumers that read frontmatter outside this module tree.
const MaxFrontmatterBytes = yamlfm.MaxFrontmatterBytes

// ErrFrontmatterTooLarge reports frontmatter above MaxFrontmatterBytes.
var ErrFrontmatterTooLarge = yamlfm.ErrFrontmatterTooLarge
