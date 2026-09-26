package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/codemode"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func newPromoteCmd() *cobra.Command {
	var scopeStr string
	cmd := &cobra.Command{
		Use:   "promote <ref>",
		Short: "Install a resource discovered through code-mode",
		Long: "promote looks up the most recent code-mode session-log entry for the given " +
			"catalog ref and installs that resource through the normal install and lock " +
			"transaction, so a dynamically used skill or MCP server becomes a durable selection.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := pipeline.ParseScope(scopeStr)
			if err != nil {
				return err
			}
			return runPromote(cmd.Context(), cmd.OutOrStdout(), cmd.InOrStdin(), args[0], scope, cmd.Flags().Changed("scope"))
		},
	}
	cmd.Flags().StringVar(&scopeStr, "scope", "", "Config layer to write to: repo, user, or local")
	return cmd
}

var sessionLogDir = func() string {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(root, "agent-marketplace", "mcp-sessions")
}

func runPromote(ctx context.Context, out io.Writer, in io.Reader, ref string, scope pipeline.Scope, scopeSpecified bool) error {
	entry, err := latestSessionEntry(ref)
	if err != nil {
		return err
	}
	kind := refKind(entry.Ref)
	if kind == "" {
		return fmt.Errorf("cannot determine resource kind from ref %q", entry.Ref)
	}
	if entry.MarketplaceProvenance() {
		locator := entry.Ref
		if kind == "mcp" {
			parts := strings.SplitN(locator, "/", 3)
			if len(parts) != 3 {
				return fmt.Errorf("malformed recorded Marketplace ref %q", entry.Ref)
			}
			locator = parts[0] + "/mcpserver/" + parts[2]
		}
		if entry.Digest != "" {
			locator += "@" + entry.Digest
		}
		return runInstall(ctx, out, in, false, false, []string{locator}, "", false, "", scope, scopeSpecified, "", "", "", "", "", true, "")
	}
	name := entry.Ref[strings.LastIndex(entry.Ref, "/")+1:]
	if entry.Source != "" {
		return runInstall(ctx, out, in, false, false, []string{name}, "", false, "", scope, scopeSpecified, kind, entry.Source, entry.Revision, entry.SourcePath, "", true, "")
	}
	if strings.Contains(entry.Ref, ":") && !strings.HasPrefix(entry.Ref, "local:") {
		return fmt.Errorf("ref %q has no recorded provenance and cannot be routed safely; reload it through code mode first", entry.Ref)
	}
	return runInstall(ctx, out, in, false, false, []string{name}, "", false, "", scope, scopeSpecified, kind, "", "", "", "", true, "")
}

func refKind(ref string) string {
	if idx := strings.Index(ref, ":"); idx >= 0 {
		ref = ref[idx+1:]
	}
	parts := strings.Split(ref, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2]
}

func latestSessionEntry(ref string) (codemode.SessionEntry, error) {
	dir := sessionLogDir()
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(files) == 0 {
		return codemode.SessionEntry{}, fmt.Errorf("no code-mode session log records ref %q (nothing loaded via code mode yet)", ref)
	}
	var best codemode.SessionEntry
	var bestTime time.Time
	found := false
	for _, file := range files {
		entries, err := codemode.ReadSession(file)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Ref == ref && (e.Event == "skill.load" || e.Event == "server.start") && e.Timestamp.After(bestTime) {
				best, bestTime, found = e, e.Timestamp, true
			}
		}
	}
	if !found {
		return codemode.SessionEntry{}, fmt.Errorf("no code-mode session log records ref %q", ref)
	}
	return best, nil
}
