package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/Adyen/agent-marketplace/internal/telemetry/receiver"
)

func newTelemetryReceiverCmd() *cobra.Command {
	var (
		listen           string
		storageDir       string
		maxInstallations int
		maxSeries        int
		maxRequestIDs    int
	)

	cmd := &cobra.Command{
		Use:   "receiver",
		Short: "Run a local OTLP/HTTP community telemetry receiver",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTelemetryReceiver(cmd.Context(), cmd.OutOrStdout(), listen, storageDir, maxInstallations, maxSeries, maxRequestIDs)
		},
	}

	cmd.Flags().StringVar(&listen, "listen", receiver.DefaultListenAddr, "Address to listen on")
	cmd.Flags().StringVar(&storageDir, "storage-dir", "", "Directory to store received telemetry (default $XDG_STATE_HOME/.../agent-marketplace/telemetry/receiver or ~/.local/state/.../agent-marketplace/telemetry/receiver)")
	cmd.Flags().IntVar(&maxInstallations, "max-installations", telemetry.DefaultInstallationIDCardinalityBudget, "Maximum number of unique installation IDs to accept")
	cmd.Flags().IntVar(&maxSeries, "max-series", receiver.DefaultPrometheusSeriesBudget, "Maximum number of Prometheus adoption series to retain")
	cmd.Flags().IntVar(&maxRequestIDs, "max-request-ids", receiver.DefaultRequestIDBudget, "Maximum number of retry request IDs to retain")

	return cmd
}

func runTelemetryReceiver(ctx context.Context, out io.Writer, listen, storageDir string, maxInstallations, maxSeries, maxRequestIDs int) error {
	if storageDir == "" {
		dir, err := receiver.DefaultStorageDir()
		if err != nil {
			return fmt.Errorf("resolve default storage directory: %w", err)
		}
		storageDir = dir
	}

	srv, err := receiver.New(receiver.Config{
		ListenAddr:       listen,
		StorageDir:       storageDir,
		MaxInstallations: maxInstallations,
		MaxSeries:        maxSeries,
		MaxRequestIDs:    maxRequestIDs,
		Logger:           slog.Default(),
	})
	if err != nil {
		return fmt.Errorf("create receiver: %w", err)
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	_, _ = fmt.Fprintln(out, "Telemetry receiver starting...")
	_, _ = fmt.Fprintf(out, "  Listen address:  http://%s\n", listen)
	_, _ = fmt.Fprintf(out, "  Dashboard URL:   http://%s/v1/dashboard.json\n", listen)
	_, _ = fmt.Fprintf(out, "  Prometheus URL:  http://%s/metrics\n", listen)
	_, _ = fmt.Fprintf(out, "  Storage dir:     %s\n", storageDir)

	if err := srv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("receiver stopped: %w", err)
	}
	return nil
}
