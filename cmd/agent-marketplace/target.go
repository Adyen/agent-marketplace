package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

type targetApprovalDisplay struct {
	OS                 string                          `json:"os"`
	Profile            targetprofile.Identity          `json:"profile"`
	CanonicalRoot      string                          `json:"canonical_root"`
	CanonicalStateRoot string                          `json:"canonical_state_root"`
	StateRootKey       string                          `json:"state_root_key"`
	UsedOverride       bool                            `json:"used_override"`
	CommandRoles       []string                        `json:"command_roles"`
	Executable         targetprofile.ExecutableBinding `json:"executable"`
	NoExecution        bool                            `json:"no_execution"`
	InstallationID     string                          `json:"installation_id"`
}

type targetApprovalResult struct {
	Status  string                `json:"status"`
	Binding targetApprovalDisplay `json:"binding"`
}

func newTargetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "target", Short: "Manage target-profile selection and approval"}
	cmd.AddCommand(newTargetApproveCmd(), newTargetToggleCmd("enable"), newTargetToggleCmd("disable"), newTargetVerifyCmd())
	return cmd
}

func newTargetToggleCmd(action string) *cobra.Command {
	enable := action == "enable"
	return &cobra.Command{
		Use:  action + " <target-id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := discoverSyncWorkspace()
			if err != nil {
				return err
			}
			changed, err := pipeline.SetDeployedPlaceTargetEnabled(cmd.Context(), ws, args[0], enable)
			if err != nil {
				return err
			}
			state := "disabled"
			if enable {
				state = "enabled"
			}
			if !changed {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Target %s already %s.\n", args[0], state)
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Target %s %s in user config.\n", args[0], state)
			return err
		},
	}
}

func newTargetApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "approve <target-id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("yes") {
				return fmt.Errorf("target.confirmation-required: --yes is not supported for target approval")
			}
			in := cmd.InOrStdin()
			file, ok := in.(*os.File)
			if !ok || !isTerminal(file.Fd()) {
				return fmt.Errorf("target.confirmation-required: target approval requires a terminal")
			}
			ws, err := discoverSyncWorkspace()
			if err != nil {
				return err
			}
			before, err := pipeline.PrepareDeployedPlaceApproval(cmd.Context(), ws, args[0])
			if err != nil {
				return err
			}
			display := approvalDisplay(before)
			presentation, err := json.MarshalIndent(display, "", "  ")
			if err != nil {
				return fmt.Errorf("encode approval binding: %w", err)
			}
			if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Approval binding (no host commands will run; approval does not enable the target):\n%s\nType yes to approve: ", presentation); err != nil {
				return err
			}
			answer, readErr := bufio.NewReaderSize(in, 64).ReadSlice('\n')
			if readErr != nil || strings.TrimSpace(string(answer)) != "yes" {
				if readErr != nil && !errors.Is(readErr, io.EOF) {
					return fmt.Errorf("target.confirmation-required: approval confirmation must be a complete line containing yes")
				}
				return fmt.Errorf("target.confirmation-required: approval was not confirmed")
			}
			current, err := pipeline.PrepareDeployedPlaceApproval(cmd.Context(), ws, args[0])
			if err != nil {
				return err
			}
			if err := before.Match(current); err != nil {
				return fmt.Errorf("target.approval.context-changed: %w", err)
			}
			if err := cmd.Context().Err(); err != nil {
				return fmt.Errorf("target.confirmation-required: approval interrupted: %w", err)
			}
			if err := targetprofile.RecordApproval(current); err != nil {
				return err
			}
			result := targetApprovalResult{Status: "approved", Binding: approvalDisplay(current)}
			if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Target %s approval recorded; future installation ID for this approved root: %s. This does not enable or install the target.\n",
				current.Profile.TargetID, pipeline.PlaceInstallationID(current))
			return err
		},
	}
	cmd.Flags().Bool("yes", false, "Skip confirmation (not supported)")
	cmd.Flags().Bool("json", false, "Print the approval result as JSON")
	return cmd
}

func newTargetVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "verify <target-id>%2F<kind>/<name>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kindToken, name, hasSeparator := strings.Cut(args[0], "/")
			if !hasSeparator || strings.Contains(name, "/") {
				return fmt.Errorf("target.place.package-invalid: expected one target-kind/package-name separator")
			}
			kind, ok := nameutil.DecodePlaceKindToken(kindToken)
			if !ok || !nameutil.IsPlacePackageName(name) {
				return fmt.Errorf("target.place.package-invalid: target kind token or package name is invalid")
			}
			installationID, _ := cmd.Flags().GetString("installation")
			if installationID == "" {
				return fmt.Errorf("target.place.installation-unavailable: --installation is required")
			}
			ws, err := discoverSyncWorkspace()
			if err != nil {
				return err
			}
			result, verifyErr := pipeline.VerifyDeployedPlace(cmd.Context(), ws, kind, name, installationID)
			if result.Outcome != "" {
				if err := writePlaceVerificationResult(cmd.OutOrStdout(), kind, name, result); err != nil {
					return errors.Join(verifyErr, err)
				}
			}
			return verifyErr
		},
	}
	cmd.Flags().String("installation", "", "Current approved-root installation ID")
	_ = cmd.MarkFlagRequired("installation")
	return cmd
}

func writePlaceVerificationResult(out io.Writer, kind, name string, result ownership.PlaceVerification) error {
	version := result.HostVersionStatus
	if result.HostVersion != "" {
		version = result.HostVersion + " (" + result.HostVersionStatus + ")"
	}
	_, err := fmt.Fprintf(out,
		"Verified %s/%s with check %s: %s (package %s, profile %s, generation %s, host version %s, evidence %s).\n",
		kind, name, result.Check, result.Outcome,
		result.Binding.PackageContentDigest, result.Binding.ProfileIdentity.RevisionDigest,
		result.GenerationKey, version, result.EvidenceSource,
	)
	return err
}

func approvalDisplay(binding targetprofile.ApprovalBinding) targetApprovalDisplay {
	return targetApprovalDisplay{
		OS: binding.OS, Profile: binding.Profile, CanonicalRoot: binding.CanonicalRoot,
		CanonicalStateRoot: binding.CanonicalStateRoot, StateRootKey: binding.StateRootKey,
		UsedOverride: binding.UsedOverride, CommandRoles: binding.CommandRoles,
		Executable: binding.Executable, NoExecution: len(binding.CommandRoles) == 0,
		InstallationID: pipeline.PlaceInstallationID(binding),
	}
}
