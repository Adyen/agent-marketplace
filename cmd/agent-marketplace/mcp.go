package main

import (
	"context"
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
		Short: "Run the code-mode MCP server over stdio",
		Long: "agent-marketplace mcp serves the code-mode MCP surface (search and execute) " +
			"over stdio. It is started and stopped by the harness that registered it. " +
			"Register it by installing the code-mode collection from the agent-marketplace registry.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPServer(cmd.Context(), os.Stdin, cmd.OutOrStdout())
		},
	}
	return cmd
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
