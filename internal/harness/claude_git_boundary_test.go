package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// spec: claude-native-configuration/git-administrative-roots-refuse
func TestClaudeConfigurationGitAdministrativeRootsRefuse(t *testing.T) {
	for _, shape := range []string{"literal", "git-dir", "common-env", "worktree", "common", "alias", "case-alias"} {
		t.Run(shape, func(t *testing.T) {
			home, repo, control := t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("GIT_DIR", "")
			t.Setenv("GIT_COMMON_DIR", "")
			gitDir := filepath.Join(control, "worktrees", "one")
			if err := os.MkdirAll(gitDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(control, "account")
			switch shape {
			case "literal":
				root = filepath.Join(repo, ".git", "account")
			case "git-dir", "case-alias":
				t.Setenv("GIT_DIR", control)
				if shape == "case-alias" {
					root = filepath.Join(strings.ToUpper(control), "account")
				}
			case "common-env":
				t.Setenv("GIT_COMMON_DIR", control)
			case "worktree", "common", "alias":
				if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if shape == "worktree" {
					root = filepath.Join(gitDir, "account")
				}
				if shape == "alias" {
					alias := filepath.Join(t.TempDir(), "alias")
					if err := os.Symlink(control, alias); err != nil {
						t.Fatal(err)
					}
					root = filepath.Join(alias, "account")
				}
			}
			t.Setenv("CLAUDE_CONFIG_DIR", root)
			if _, err := ResolveDestinations(hcc.TargetClaude, true, repo); err == nil || !strings.Contains(err.Error(), "Git administrative") {
				t.Fatalf("Git administrative root admitted: %s: %v", root, err)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatalf("refused root was created: %v", err)
			}
		})
	}
}

// spec: claude-native-configuration/nonadministrative-roots-remain-eligible
func TestClaudeConfigurationGitBoundaryPreservesDisjointRoots(t *testing.T) {
	for _, ancestor := range []bool{false, true} {
		t.Run(map[bool]string{false: "repo-sibling", true: "ancestor"}[ancestor], func(t *testing.T) {
			home, repo := t.TempDir(), t.TempDir()
			admin := filepath.Join(repo, ".git")
			if err := os.MkdirAll(admin, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("GIT_DIR", admin)
			t.Setenv("GIT_COMMON_DIR", "")
			root := filepath.Join(repo, "account")
			if ancestor {
				root = repo
			}
			t.Setenv("CLAUDE_CONFIG_DIR", root)
			if destinations, err := ResolveDestinations(hcc.TargetClaude, true, repo); err != nil || len(destinations) != 2 {
				t.Fatalf("nonadministrative root refused: %v, %+v", err, destinations)
			}
		})
	}
}
