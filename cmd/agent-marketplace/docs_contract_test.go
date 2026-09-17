package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

var docsHeadingRe = regexp.MustCompile("(?m)^#{2,3} `([a-z][a-z -]*)`$")

// TestDocsContract keeps site/docs/reference in sync with the binary: every
// command, flag, harness identifier, environment variable, and resource kind the
// CLI exposes must be documented, and documented commands must still exist.
func TestDocsContract(t *testing.T) {
	root := newRootCmd()

	t.Run("cli", func(t *testing.T) {
		doc := readDoc(t, "site/docs/reference/cli.md")
		global := section(doc, "## Global flags")
		checkFlags := func(t *testing.T, where string, text string, flags *pflag.FlagSet) {
			flags.VisitAll(func(f *pflag.Flag) {
				if f.Hidden || f.Name == "help" {
					return
				}
				if !strings.Contains(text, "`--"+f.Name+"`") {
					t.Errorf("%s: flag `--%s` not documented", where, f.Name)
				}
			})
		}
		checkFlags(t, "## Global flags", global, root.PersistentFlags())
		checkFlags(t, "## Global flags", global, root.LocalFlags())

		var walk func(cmd *cobra.Command)
		walk = func(cmd *cobra.Command) {
			for _, sub := range cmd.Commands() {
				if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
					continue
				}
				path := strings.TrimPrefix(sub.CommandPath(), root.Name()+" ")
				heading := "`" + path + "`"
				var sec string
				if sub.HasParent() && sub.Parent() != root {
					sec = section(doc, "### "+heading)
				} else {
					sec = section(doc, "## "+heading)
				}
				if sec == "" {
					t.Errorf("cli.md: no heading for command %q", path)
				} else {
					checkFlags(t, path, sec, sub.LocalFlags())
				}
				walk(sub)
			}
		}
		walk(root)

		for _, m := range docsHeadingRe.FindAllStringSubmatch(doc, -1) {
			path := m[1]
			if path == "completion" {
				continue
			}
			found, _, err := root.Find(strings.Fields(path))
			if err != nil || found == nil || found == root || strings.TrimPrefix(found.CommandPath(), root.Name()+" ") != path {
				t.Errorf("cli.md: heading `%s` does not match an existing command", path)
			}
		}
	})

	t.Run("harnesses", func(t *testing.T) {
		harnessDoc := readDoc(t, "site/docs/reference/harnesses-and-output-paths.md")
		configDoc := readDoc(t, "site/docs/reference/configuration.md")
		names := hcc.TargetNames()
		for _, name := range names {
			for file, doc := range map[string]string{"harnesses-and-output-paths.md": harnessDoc, "configuration.md": configDoc} {
				if !strings.Contains(doc, "`"+name+"`") {
					t.Errorf("%s: harness `%s` not documented", file, name)
				}
			}
		}
		rows := 0
		for _, line := range strings.Split(section(harnessDoc, "## Supported harness identifiers"), "\n") {
			if strings.HasPrefix(line, "|") && strings.Trim(line, "|-: ") != "" {
				rows++
			}
		}
		if got := rows - 1; got != len(names) {
			t.Errorf("harnesses-and-output-paths.md: identifier table has %d rows, binary has %d targets %v", got, len(names), names)
		}
	})

	t.Run("environment", func(t *testing.T) {
		doc := readDoc(t, "site/docs/reference/environment-variables.md")
		want := map[string]bool{
			"AGENT_MARKETPLACE_NO_TUI":           true,
			"AGENT_MARKETPLACE_DEBUG":            true,
			"AGENT_MARKETPLACE_LOG_ENABLED":      true,
			"AGENT_MARKETPLACE_LOG_LEVEL":        true,
			"AGENT_MARKETPLACE_LOG_FILE":         true,
			"AGENT_MARKETPLACE_LOG_MAX_SIZE_MB":  true,
			"AGENT_MARKETPLACE_LOG_MAX_BACKUPS":  true,
			"AGENT_MARKETPLACE_LOG_MAX_AGE_DAYS": true,
			"AGENT_MARKETPLACE_LOG_COMPRESS":     true,
			telemetry.EnvLevel:                   true,
			telemetry.EnvEndpoint:                true,
			telemetry.EnvCommunityReporting:      true,
			ownership.LockTimeoutEnvVar:          true,
			"CODEX_HOME":                         true,
			"XDG_STATE_HOME":                     true,
		}
		envRe := regexp.MustCompile(`"(AGENT_MARKETPLACE_[A-Z_]+)"`)
		for _, dir := range []string{"internal", "cmd"} {
			err := filepath.WalkDir(filepath.Join(repoRoot(t), dir), func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				for _, m := range envRe.FindAllStringSubmatch(string(data), -1) {
					want[m[1]] = true
				}
				return nil
			})
			if err != nil {
				t.Fatalf("scan %s: %v", dir, err)
			}
		}
		for name := range want {
			if !strings.Contains(doc, "`"+name+"`") {
				t.Errorf("environment-variables.md: `%s` not documented", name)
			}
		}
	})

	t.Run("kinds", func(t *testing.T) {
		kinds := section(readDoc(t, "site/docs/reference/cli.md"), "## Resource kinds")
		for _, k := range []string{"skill", "command", "subagent", "hook", "mcp", "collection"} {
			if !strings.Contains(kinds, "`"+k+"`") {
				t.Errorf("cli.md Resource kinds: `%s` not documented", k)
			}
		}
		if doc := readDoc(t, "site/docs/reference/marketplace-resource-locators.md"); !strings.Contains(doc, "`mcpserver`") {
			t.Errorf("marketplace-resource-locators.md: `mcpserver` not documented")
		}
	})
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above working directory")
		}
		dir = parent
	}
}

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func section(doc, heading string) string {
	idx := strings.Index(doc, "\n"+heading+"\n")
	if idx < 0 {
		return ""
	}
	body := doc[idx+1+len(heading)+1:]
	stop := regexp.MustCompile("(?m)^## ")
	if strings.HasPrefix(heading, "### ") {
		stop = regexp.MustCompile("(?m)^##{1,2} ")
	}
	if m := stop.FindStringIndex(body); m != nil {
		body = body[:m[0]]
	}
	return body
}
