package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func newServeAdoptCmd() *cobra.Command {
	var path, actor, reason, key string
	var confirmed bool
	cmd := &cobra.Command{
		Use: "adopt", Short: "Adopt catalog settings during a controlled maintenance window", Args: cobra.NoArgs,
		Long: `Adopt validates catalog settings, completes retained blob ownership backfill,
and records database configuration authority. Stop every API and worker process,
back up metadata and blobs, and invalidate controllable shared caches first.
This command does not verify external process shutdown or cache invalidation.
Managed runtime activation remains subject to this release's safety gate.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !confirmed {
				return errors.New("adoption requires --confirm-maintenance after stopping all API/worker processes, backing up metadata/blobs, and invalidating shared caches")
			}
			for _, value := range []string{path, actor, reason, key} {
				if strings.TrimSpace(value) == "" {
					return errors.New("config, actor, reason, and idempotency-key must be nonempty")
				}
			}
			cfg, err := mpconfig.LoadConfig(path)
			if err != nil {
				return fmt.Errorf("load adoption configuration: %w", err)
			}
			if cfg.Management.Mode != "database" {
				return errors.New("adoption requires management.mode: database")
			}
			meta, blobs, err := mpconfig.OpenStores(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("open adoption stores: %w", err)
			}
			defer func() { _ = meta.Close() }()
			service := admin.Service{Metadata: meta, Deployment: cfg.Management}
			change, err := service.AdoptMaintenance(cmd.Context(), cfg, blobs, admin.AdoptionRequest{Actor: store.ManagedActor{Issuer: "marketplace-maintenance", Subject: actor}, Reason: reason, RequestID: key, IdempotencyKey: key})
			if err != nil {
				return fmt.Errorf("adopt marketplace configuration: %w", err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Adopted marketplace configuration at revision %d. Database authority is durable; runtime safety checks still apply.\n", change.Revision)
			return err
		},
	}
	cmd.Flags().StringVar(&path, "config", "", "Path to the Marketplace operator config file")
	cmd.Flags().StringVar(&actor, "actor", "", "Operator identity recorded in the maintenance audit")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason recorded in the adoption audit")
	cmd.Flags().StringVar(&key, "idempotency-key", "", "Stable key reused when retrying this adoption")
	cmd.Flags().BoolVar(&confirmed, "confirm-maintenance", false, "Confirm all API/workers are stopped, backups exist, and shared caches are invalidated")
	for _, name := range []string{"config", "actor", "reason", "idempotency-key"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}
