package session

import (
	"context"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
)

// MigrationConfig is one legacy config file the startup migration offer may
// carry.
type MigrationConfig struct {
	Label string
	Path  string
	Scope config.V4MigrationScope
}

// MigrationPrompt is the set of legacy configs offered for migration.
type MigrationPrompt struct {
	Configs []MigrationConfig
}

func (p MigrationPrompt) HasConfigs() bool {
	return len(p.Configs) > 0
}

// PendingMigrationFor reports legacy configs the startup migration modal may
// offer. Startup migration only migrates the user config; repository and local
// configs are left unchanged until migrated deliberately. The repoRoot and
// localDir parameters keep the validated Workspace roots available at this seam.
func PendingMigrationFor(repoRoot, localDir, homeDir string) MigrationPrompt {
	paths := config.MigratePaths{}
	if homeDir != "" {
		paths.User = filepath.Join(homeDir, ".agents", "config.yaml")
	}
	candidates, err := config.FindV4MigrationCandidates(paths)
	if err != nil {
		return MigrationPrompt{}
	}
	labels := map[config.V4MigrationScope]string{
		config.V4MigrationUser:  "Global",
		config.V4MigrationRepo:  "Project",
		config.V4MigrationLocal: "Local",
	}
	configs := make([]MigrationConfig, 0, len(candidates))
	for _, candidate := range candidates {
		configs = append(configs, MigrationConfig{
			Label: labels[candidate.Scope],
			Path:  candidate.Path,
			Scope: candidate.Scope,
		})
	}
	return MigrationPrompt{Configs: configs}
}

func migrationPathsForPrompt(prompt MigrationPrompt) config.MigratePaths {
	paths := config.MigratePaths{}
	for _, cfg := range prompt.Configs {
		switch cfg.Scope {
		case config.V4MigrationUser:
			paths.User = cfg.Path
		case config.V4MigrationRepo:
			paths.Repo = cfg.Path
		case config.V4MigrationLocal:
			paths.Local = cfg.Path
		}
	}
	return paths
}

// RunMigration migrates the paths carried by the prompt and returns how
// many roots changed.
func RunMigration(ctx context.Context, prompt MigrationPrompt) (int, error) {
	paths := migrationPathsForPrompt(prompt)
	result, err := config.MigrateConfigFilesToV4WithEvidence(ctx, paths, config.V4MigrationAll, false, marketplace.DefaultCache())
	count := 0
	if result != nil {
		if result.UserMigrated {
			count++
		}
		if result.RepoMigrated {
			count++
		}
		if result.LocalMigrated {
			count++
		}
	}
	return count, err
}
