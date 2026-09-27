//go:build !e2e

package main

import "github.com/spf13/cobra"

// Production builds carry no E2E command-coverage hooks; see
// command_coverage_e2e.go.

func writeE2ECommandTree(*cobra.Command) bool { return false }

func e2eConfigSchemas() map[string]string { return nil }

func recordE2EInvocation(_, _ *cobra.Command, _ map[string]string, _ error) {}
