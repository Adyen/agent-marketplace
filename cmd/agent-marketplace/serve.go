package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/spf13/cobra"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/worker"
)

func newServeCmd() *cobra.Command {
	var cfgPath string
	var role string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Marketplace server",
		Long: `serve loads the Marketplace operator configuration, opens the configured
metadata and blob stores, and runs the requested server role(s). Supported roles
are "worker" (background indexing), "api" (HTTP API), and "all" (both).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			slog.Info("serve starting", "role", role, "config", cfgPath, "version", version)
			if cfgPath == "" {
				slog.Error("serve failed", "reason", "--config is required")
				return fmt.Errorf("--config is required")
			}
			if role != "worker" && role != "api" && role != "all" {
				slog.Error("serve failed", "reason", "unsupported role", "role", role)
				return fmt.Errorf("role %q is not supported; use --role worker|api|all", role)
			}

			cfg, err := mpconfig.LoadConfig(cfgPath)
			if err != nil {
				slog.Error("serve failed", "reason", "load operator config", "error", err)
				return fmt.Errorf("load operator config: %w", err)
			}

			ctx := cmd.Context()
			meta, blobs, err := mpconfig.OpenStores(ctx, cfg)
			if err != nil {
				slog.Error("serve failed", "reason", "open stores", "error", err)
				return fmt.Errorf("open stores: %w", err)
			}
			defer func() { _ = meta.Close() }()

			deps := worker.Deps{
				Config:        cfg,
				MetadataStore: meta,
				BlobStore:     blobs,
			}
			if err := worker.EnsureMarketplace(ctx, &deps); err != nil {
				slog.Error("serve failed", "reason", "ensure marketplace", "error", err)
				return fmt.Errorf("ensure marketplace: %w", err)
			}
			// Apply live exact revocations before either role can announce or
			// serve readiness. The API overlays the same declarations on reads;
			// this preflight also covers API-only deployments.
			if err := worker.EnsureRevocations(ctx, deps); err != nil {
				slog.Error("serve failed", "reason", "ensure revocations", "error", err)
				return fmt.Errorf("ensure revocations: %w", err)
			}

			slog.Info("serve running", "marketplace", cfg.Instance.ID, "role", role, "registries", len(cfg.Registries))

			var wg sync.WaitGroup
			errCh := make(chan error, 2)

			if role == "worker" || role == "all" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := worker.Run(ctx, deps, cfg); err != nil && !errors.Is(err, context.Canceled) {
						slog.Error("worker stopped", "error", err)
						errCh <- fmt.Errorf("worker run: %w", err)
					}
				}()
			}

			if role == "api" || role == "all" {
				wg.Add(1)
				go func() {
					defer wg.Done()
					server := mpapi.NewServer(cfg, meta, blobs, mpapi.WithVersion(version))
					if err := server.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
						slog.Error("api server stopped", "error", err)
						errCh <- fmt.Errorf("api server: %w", err)
					}
				}()
			}

			wg.Wait()
			close(errCh)
			for err := range errCh {
				if err != nil {
					slog.Error("serve stopped with error", "marketplace", cfg.Instance.ID, "error", err)
					return err
				}
			}
			slog.Info("serve stopped", "marketplace", cfg.Instance.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&cfgPath, "config", "", "Path to the Marketplace operator config file")
	cmd.Flags().StringVar(&role, "role", "all", "Server role (worker|api|all)")
	_ = cmd.MarkFlagRequired("config")

	cmd.AddCommand(newServeReindexCmd(), newServeAdoptCmd())
	return cmd
}

func newServeReindexCmd() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "reindex [registry-slug-or-id]",
		Short: "Enqueue and process one refresh job for a registry",
		Long: `reindex loads the Marketplace operator configuration, opens the stores,
optionally enqueues and waits for the indexing work for a single registry. If no
registry is provided, all configured registries are reindexed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			registryArg := ""
			if len(args) == 1 {
				registryArg = args[0]
			}
			slog.Info("reindex starting", "config", cfgPath, "registry", registryArg)
			if cfgPath == "" {
				slog.Error("reindex failed", "reason", "--config is required")
				return fmt.Errorf("--config is required")
			}
			if len(args) > 1 {
				slog.Error("reindex failed", "reason", "too many arguments")
				return errors.New("accepts at most one registry slug or id")
			}

			cfg, err := mpconfig.LoadConfig(cfgPath)
			if err != nil {
				slog.Error("reindex failed", "reason", "load operator config", "error", err)
				return fmt.Errorf("load operator config: %w", err)
			}
			if cfg.Management.Mode != "database" && len(args) == 1 && !hasRegistry(cfg, args[0]) {
				err := fmt.Errorf("registry %q not found in config", args[0])
				slog.Error("reindex failed", "registry", args[0], "error", err)
				return err
			}

			ctx := cmd.Context()
			meta, blobs, err := mpconfig.OpenStores(ctx, cfg)
			if err != nil {
				slog.Error("reindex failed", "reason", "open stores", "error", err)
				return fmt.Errorf("open stores: %w", err)
			}
			defer func() { _ = meta.Close() }()

			deps := worker.Deps{
				Config:        cfg,
				MetadataStore: meta,
				BlobStore:     blobs,
			}
			// Reindex is a worker-role operation too: do not perform registry
			// acquisition while the live revocation desired set is stale.
			if err := worker.EnsureRevocations(ctx, deps); err != nil {
				slog.Error("reindex failed", "reason", "ensure revocations", "error", err)
				return fmt.Errorf("ensure revocations: %w", err)
			}

			ctx, cancel := context.WithTimeout(ctx, time.Hour)
			defer cancel()

			targets, err := reindexTargets(ctx, meta, cfg, registryArg)
			if err != nil {
				return fmt.Errorf("select reindex targets: %w", err)
			}
			var reindexErrs []error
			for _, target := range targets {
				slog.Info("reindexing registry", "registry", target)
				if err := worker.Reindex(ctx, deps, cfg, target); err != nil {
					slog.Error("reindex failed", "registry", target, "error", err)
					reindexErrs = append(reindexErrs, fmt.Errorf("reindex registry %q: %w", target, err))
				}
			}

			if err := meta.CheckIntegrity(ctx, blobs); err != nil {
				slog.Error("reindex failed", "reason", "integrity check", "error", err)
				reindexErrs = append(reindexErrs, err)
			}
			if err := errors.Join(reindexErrs...); err != nil {
				return err
			}
			slog.Info("reindex complete", "registries", len(targets))
			return nil
		},
	}

	cmd.Flags().StringVar(&cfgPath, "config", "", "Path to the Marketplace operator config file")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}

func hasRegistry(cfg mpconfig.Config, slugOrID string) bool {
	for _, registry := range cfg.Registries {
		if registry.ID == slugOrID || registry.Slug == slugOrID {
			return true
		}
	}
	return false
}
