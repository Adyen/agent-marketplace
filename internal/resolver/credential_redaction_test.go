package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

func TestMalformedSelectionCredentialRedaction(t *testing.T) {
	raw := "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.invalid/repo.git?token=YOUR_QUERY_HERE#YOUR_FRAGMENT_HERE"
	cfg := &config.Config{Registries: []config.RegistryEntry{{URL: raw, Ref: "main", Skills: ks("broken")}}}
	key := ck(raw, "main")
	cause := errors.New("synthetic invalid frontmatter")
	setCatalogKeys(cfg)
	_, errs := Resolve(cfg, map[string][]catalog.Entry{key: {}}, nil, WithMalformed(malformedFixture(key, skill.MalformedSkill{
		DirName: "broken", Path: "/cache/fixture/skills/broken", Error: cause,
	})))
	var malformed *SelectedMalformedError
	if len(errs) != 1 || !errors.As(errs[0], &malformed) || malformed.URL != raw || !errors.Is(errs[0], cause) {
		t.Fatalf("malformed-selection identity lost: %v", errs)
	}
	text := errs[0].Error() + "\n" + malformed.Remediation()
	for _, expected := range []string{"example.invalid/repo.git", "broken", "synthetic invalid frontmatter"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing diagnostic context %q", expected)
		}
	}
	for _, secret := range []string{"YOUR_USER_HERE", "YOUR_TOKEN_HERE", "YOUR_QUERY_HERE", "YOUR_FRAGMENT_HERE"} {
		if strings.Contains(text, secret) {
			t.Errorf("malformed selection exposed %s", secret)
		}
	}
}
