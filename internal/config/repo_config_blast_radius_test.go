package config_test

import (
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

// TestCommittedRepoConfigDoesNotRequireAnything guards the blast radius of this
// repository's own committed `.agents/config.yaml`: every developer who clones
// the repo inherits it.
//
// `required` is the thing a contributor cannot opt out of, and it is reserved
// for the organization layer (/etc/agent-marketplace/config.yaml) — a project
// config has no authority to make a resource unexcludable on someone else's
// machine.
//
// `default: true` is deliberately NOT forbidden here. It auto-selects the
// resources it finds, but a contributor can still exclude any of them, so it is
// a reasonable way for a repo to offer its own skills. This repo uses it for
// exactly that.
func TestCommittedRepoConfigDoesNotRequireAnything(t *testing.T) {
	cfg, err := config.LoadConfigFromPath("../../.agents/config.yaml")
	if err != nil {
		t.Fatalf("load committed repo config: %v", err)
	}

	for _, entry := range cfg.Registries {
		if entry.Required {
			t.Errorf("committed repo config marks %q (path %q) required: every contributor is forced to materialize it and cannot exclude it; required belongs in the organization config", entry.URL, entry.Path)
		}
	}
}
