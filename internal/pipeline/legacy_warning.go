package pipeline

import (
	"log/slog"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func warnLegacyLayerMutation(source config.Source, path string) {
	next := "agent-marketplace migrate"
	switch source {
	case config.UserPersonal:
		next = "agent-marketplace migrate --scope user"
	case config.RepoCommitted:
		next = "agent-marketplace migrate --scope repo"
	case config.RepoPersonal:
		next = "agent-marketplace migrate --scope local"
	}
	slog.Warn("config layer uses the deprecated v2/v3 schema; migrate it to v4",
		slog.String("code", "config.legacy-deprecated"),
		slog.String("path", path),
		slog.String("source", source.String()),
		slog.String("next_command", next),
	)
}
