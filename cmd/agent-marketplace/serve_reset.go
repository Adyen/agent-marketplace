package main

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func newServeResetCmd() *cobra.Command {
	var path string
	var dryRun, confirmed bool
	cmd := &cobra.Command{
		Use:   "reset <registry-slug-or-id>",
		Short: "Reset one registry's generated catalog during maintenance",
		Long: `Reset removes one registry's published resources, assessments, collections,
occurrences and publication history in a metadata transaction. Registry settings,
managed configuration, policies, revocations, administrative history, other
registries and all blob contents are preserved. Nothing is reindexed automatically.

Stop every API and worker process, back up metadata, and invalidate controllable
shared caches before confirming maintenance. This command refuses live job leases
but cannot verify process shutdown or external cache invalidation. The registry
is unavailable until serve reindex succeeds; old locks and cached revision metadata
may need refreshing. Reset does not make old clients compatible with new artifacts.
Use --dry-run to preview the affected row counts without resetting catalog data.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			if !dryRun && !confirmed {
				return errors.New("reset requires --confirm-maintenance after stopping all API/workers, backing up metadata, and invalidating shared caches; use --dry-run to preview")
			}
			if strings.TrimSpace(args[0]) == "" || strings.TrimSpace(path) == "" {
				return errors.New("registry and config must be nonempty")
			}
			slog.Info("registry reset started", "registry", args[0], "config", path, "dry_run", dryRun)
			defer func() {
				if retErr != nil {
					slog.Error("registry reset failed", "registry", args[0], "error", retErr)
				}
			}()
			cfg, err := mpconfig.LoadConfig(path)
			if err != nil {
				return fmt.Errorf("load reset configuration: %w", err)
			}
			meta, _, err := mpconfig.OpenStores(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("open reset stores: %w", err)
			}
			defer func() { _ = meta.Close() }()
			id := store.RegistryID(args[0])
			if cfg.Management.Mode == "database" {
				targets, err := reindexTargets(cmd.Context(), meta, cfg, args[0])
				if err != nil {
					return err
				}
				id = store.RegistryID(targets[0])
			}
			registry, err := meta.GetRegistry(cmd.Context(), id)
			if errors.Is(err, store.ErrNotFound) && cfg.Management.Mode != "database" {
				registry, err = meta.GetRegistryBySlug(cmd.Context(), args[0])
			}
			if err != nil {
				return fmt.Errorf("resolve reset registry %q: %w", args[0], err)
			}
			resetter, ok := meta.(store.RegistryCatalogResetter)
			if !ok {
				return fmt.Errorf("%w: metadata store does not support catalog reset", store.ErrTopology)
			}
			counts, err := resetter.ResetRegistryCatalog(cmd.Context(), registry.ID, dryRun)
			if err != nil {
				return fmt.Errorf("reset registry %q: %w", registry.ID, err)
			}
			slog.Info("registry reset completed", "registry", registry.ID, "dry_run", dryRun)
			action := "Reset"
			if dryRun {
				action = "Would reset"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s catalog for registry %q (%s).\n", action, registry.Slug, registry.ID); err != nil {
				return err
			}
			tables := make([]string, 0, len(counts))
			for table := range counts {
				tables = append(tables, table)
			}
			sort.Strings(tables)
			for _, table := range tables {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %s: %d\n", table, counts[table]); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Registry configuration, policies, revocations and blobs are preserved. Run serve reindex for this registry to rebuild its catalog.")
			return err
		},
	}
	cmd.Flags().StringVar(&path, "config", "", "Path to the Marketplace operator config file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview generated metadata row counts without resetting them")
	cmd.Flags().BoolVar(&confirmed, "confirm-maintenance", false, "Confirm all API/workers are stopped, metadata is backed up, and shared caches are invalidated")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}
