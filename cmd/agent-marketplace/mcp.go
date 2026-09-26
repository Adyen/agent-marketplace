package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/codemode"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/mcpclient"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

func newMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the opt-in code-mode MCP server over stdio",
		Long: "agent-marketplace mcp serves the code-mode MCP surface (search and execute) " +
			"over stdio. It is started and stopped by the harness that registered it. " +
			"Enable registration with 'agent-marketplace mcp enable' or 'agent-marketplace init --code-mode'.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPServer(cmd.Context(), os.Stdin, cmd.OutOrStdout())
		},
	}

	var scopeStr string
	enable := &cobra.Command{
		Use:   "enable",
		Short: "Enable code-mode MCP registration in one config layer",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := codeModeScopePath(scopeStr)
			if err != nil {
				return err
			}
			if err := config.SetCodeModeEnabled(path, true); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "codeMode enabled in %s; run 'agent-marketplace sync' to register the server.\n", path)
			return err
		},
	}
	enable.Flags().StringVar(&scopeStr, "scope", "user", "Config layer: user, repo, or local")

	disable := &cobra.Command{
		Use:   "disable",
		Short: "Disable code-mode MCP registration in one config layer",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := codeModeScopePath(scopeStr)
			if err != nil {
				return err
			}
			if err := config.RemoveCodeMode(path); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "codeMode disabled in %s; run 'agent-marketplace sync' to unregister the server.\n", path)
			return err
		},
	}
	disable.Flags().StringVar(&scopeStr, "scope", "user", "Config layer: user, repo, or local")

	cmd.AddCommand(enable, disable)
	return cmd
}

func codeModeScopePath(scope string) (string, error) {
	switch scope {
	case "user":
		return config.GlobalConfigPath()
	case "repo":
		return config.ProjectConfigPath(config.FindRepoRoot()), nil
	case "local":
		return config.LocalConfigPath(config.FindLocalConfigDir()), nil
	default:
		return "", fmt.Errorf("unknown scope %q; expected user, repo, or local", scope)
	}
}

func runMCPServer(ctx context.Context, in io.Reader, out io.Writer) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	repoRoot := config.FindRepoRoot()
	ec := pipeline.LoadEffective(ctx, pipeline.ConfigLoadCached, config.LoadEffectiveOptions{RepoRoot: repoRoot}, nil)

	builder := &codemode.SnapshotBuilder{CatalogMode: catalog.LoadModeCached, RepoRoot: repoRoot}
	snap, errs := builder.BuildSnapshot(ctx, ec)
	for _, err := range errs {
		slog.Warn("code-mode catalog snapshot incomplete", "error", err)
	}

	store := &codemode.SnapshotStore{}
	store.Store(snap)

	loader := codemode.NewFileSkillLoader(true)
	pool := mcpclient.NewPool(mcpclient.NewSchemaCache(mcpclient.DefaultSchemaCachePath()), codemode.DefaultDeadline)
	pool.ResolveDef = func(ctx context.Context, res *codemode.Resource) (*codemode.ServerDef, error) {
		return loader.ResolveServerDef(ctx, res)
	}
	defer func() { _ = pool.Close() }()

	engine := &codemode.Engine{
		Snapshots: store,
		Skills:    loader,
		Pool:      pool,
		Log:       codemode.NewSessionLog("", uuid.NewString()),
		Limits:    codemode.Limits{},
		Gate:      loader.Gate,
	}

	go func() {
		refresh := &codemode.SnapshotBuilder{CatalogMode: catalog.LoadModeRefresh, RepoRoot: repoRoot}
		next, rerrs := refresh.BuildSnapshot(ctx, ec)
		if len(rerrs) > 0 {
			next.Stale = true
			for _, err := range rerrs {
				slog.Warn("code-mode catalog refresh incomplete", "error", err)
			}
		}
		if ctx.Err() == nil {
			store.Store(next)
		}
	}()

	server := &codemode.Server{Engine: engine, In: in, Out: out}
	return server.Serve(ctx)
}
