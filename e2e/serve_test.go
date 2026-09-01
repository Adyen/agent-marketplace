package e2e_test

import (
	"fmt"
	"testing"
)

// TestServeReindex verifies that `serve reindex` can load a Marketplace operator
// config, clone a configured registry, build artifacts, and publish a source
// revision.
func TestServeReindex(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	env.CreateBareRepo("serve-reg", map[string]string{
		".agents/skills/review/SKILL.md": "---\nname: review\ndescription: Review code carefully\nlicense: MIT\n---\n\n# review\n",
	})
	gitServer := newGitHTTPServer(t, env.repos)
	defer gitServer.Close()
	regURL := gitServer.URL + "/serve-reg.git"

	cfg := fmt.Sprintf(`apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: e2e-serve-mp
  name: E2E Serve Marketplace
  canonical_url: https://example.com
storage:
  metadata:
    driver: sqlite
    dsn: marketplace.db
  blobs:
    driver: filesystem
    path: blobs
acquisition_default: mirror
refresh:
  interval: 1h
policy:
  hidden_content: reject
  license_allowlist:
    - MIT
    - Apache-2.0
  limits:
    repository_bytes: 104857600
    file_count: 1024
    file_bytes: 2097152
    artifact_bytes: 16777216
    clone_duration: 300s
    ingestion_duration: 120s
registries:
  - id: src-serve
    slug: serve-source
    origin: %s
    ref: main
    roots:
      - path: .agents
        source_harness: factory
`, regURL)

	env.WriteFile("/workspace/marketplace.yaml", cfg)

	r := env.RunAM("serve", "reindex", "--config", "marketplace.yaml")
	if r.ExitCode != 0 {
		t.Fatalf("serve reindex exited %d:\n%s", r.ExitCode, r.Combined)
	}

	env.AssertFileExists("/workspace/marketplace.db")
	env.AssertFileExists("/workspace/blobs")
}
