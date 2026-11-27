package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type targetDryRunReport struct {
	SchemaVersion    int                                `json:"schema_version"`
	Status           string                             `json:"status"`
	Profile          targetDryRunProfile                `json:"profile"`
	Package          targetDryRunPackage                `json:"package"`
	Mappings         []targetprofile.ImportMapping      `json:"mappings"`
	ExcludedFiles    []targetprofile.ImportExcludedFile `json:"excluded_files"`
	Requirements     []targetDryRunRequirement          `json:"requirements"`
	ManualSetup      []targetDryRunSetup                `json:"manual_setup"`
	DestinationCheck targetDryRunDestinationCheck       `json:"destination_check"`
	Diagnostics      []targetDryRunDiagnostic           `json:"diagnostics"`
}

type targetDryRunProfile struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

type targetDryRunPackage struct {
	TargetID string `json:"target_id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
}

type targetDryRunRequirement struct {
	Ref    string `json:"ref"`
	Status string `json:"status"`
}

type targetDryRunSetup struct {
	Status string `json:"status"`
	Text   string `json:"text"`
}

type targetDryRunDestinationCheck struct {
	Status           string                                `json:"status"`
	Conflicts        []targetDryRunConflict                `json:"conflicts"`
	OwnershipChanges []pipeline.PlaceImportOwnershipChange `json:"ownership_changes"`
}

type targetDryRunConflict struct {
	DestinationPath string `json:"destination_path"`
	Code            string `json:"code"`
	Reason          string `json:"reason"`
}

type targetDryRunDiagnostic struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

func newTargetDryRunCmd() *cobra.Command {
	var profilePath, sourcePath, importPath, destinationPath string
	cmd := &cobra.Command{
		Use:   "dry-run",
		Short: "Validate a local target package import without changing state",
		Args:  cobra.NoArgs,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			report := targetDryRunReport{
				SchemaVersion: 1, Status: "refused",
				Mappings:      []targetprofile.ImportMapping{},
				ExcludedFiles: []targetprofile.ImportExcludedFile{},
				Requirements:  []targetDryRunRequirement{},
				ManualSetup:   []targetDryRunSetup{},
				DestinationCheck: targetDryRunDestinationCheck{
					Status: "not-requested", Conflicts: []targetDryRunConflict{},
					OwnershipChanges: []pipeline.PlaceImportOwnershipChange{},
				},
				Diagnostics: []targetDryRunDiagnostic{},
			}
			refuse := func(code, path, message string) error {
				report.Status = "refused"
				report.Diagnostics = append(report.Diagnostics, targetDryRunDiagnostic{
					Code: code, Path: path, Message: message,
				})
				return writeTargetDryRunReport(cmd, report, fmt.Errorf("%s", code))
			}
			destinationRequested := cmd.Flags().Changed("destination")
			if destinationRequested {
				report.DestinationCheck.Status = "refused"
			}
			profileBytes, err := readTargetDryRunInput(profilePath, targetprofile.MaxProfileBytes)
			if err != nil {
				return refuse("target.import.profile-invalid", profilePath, err.Error())
			}
			manifestBytes, err := readTargetDryRunInput(importPath, 1<<20)
			if err != nil {
				return refuse("target.import.manifest-invalid", importPath, err.Error())
			}
			imported, err := targetprofile.ImportLocalPackage(profileBytes, sourcePath, manifestBytes, targetprofile.PackageLimits{
				FileCount: 1024, FileBytes: 2 << 20, TotalBytes: 100 << 20,
			})
			if err != nil {
				var importErr *targetprofile.ImportError
				if errors.As(err, &importErr) {
					path := importErr.Path
					switch {
					case path == "profile":
						path = profilePath
					case path == "manifest" || strings.HasPrefix(path, "manifest."):
						path = importPath
					}
					return refuse(importErr.Code, path, importErr.Error())
				}
				return refuse("target.import.refused", sourcePath, err.Error())
			}
			report.Profile = targetDryRunProfile{
				ID: imported.Profile.ID, Revision: imported.Identity.RevisionDigest,
			}
			report.Package = targetDryRunPackage{
				TargetID: imported.TargetID, Kind: imported.Kind, Name: imported.Name,
			}
			for _, file := range imported.Files {
				report.Mappings = append(report.Mappings, targetprofile.ImportMapping{
					SourcePath: file.SourcePath, DestinationPath: file.DestinationPath,
				})
			}
			report.ExcludedFiles = append(report.ExcludedFiles, imported.ExcludedFiles...)
			for _, requirement := range imported.Metadata.Requires {
				report.Requirements = append(report.Requirements, targetDryRunRequirement{
					Ref: targetDryRunRequirementRef(requirement), Status: "unmet",
				})
			}
			for _, note := range imported.Metadata.ManualSetup {
				report.ManualSetup = append(report.ManualSetup, targetDryRunSetup{
					Status: "pending", Text: note,
				})
			}
			if destinationRequested {
				root, stateRoot, err := admitTargetDryRunDestination(destinationPath, imported)
				if err != nil {
					return refuse("target.import.destination-invalid", destinationPath, err.Error())
				}
				preview, err := pipeline.PreviewLocalPlaceImport(root, stateRoot, imported)
				if err != nil {
					return refuse("target.import.destination-invalid", destinationPath, err.Error())
				}
				report.DestinationCheck.Status = "checked"
				for _, conflict := range preview.Conflicts {
					report.DestinationCheck.Conflicts = append(report.DestinationCheck.Conflicts, targetDryRunConflict{
						DestinationPath: conflict.Path,
						Code:            "target.import.destination-conflict",
						Reason:          conflict.Reason,
					})
					report.Diagnostics = append(report.Diagnostics, targetDryRunDiagnostic{
						Code: "target.import.destination-conflict", Path: conflict.Path,
						Message: conflict.Reason,
					})
				}
				if len(preview.Conflicts) != 0 {
					report.DestinationCheck.OwnershipChanges = []pipeline.PlaceImportOwnershipChange{}
					return writeTargetDryRunReport(cmd, report, errors.New("target.import.destination-conflict"))
				}
				report.DestinationCheck.OwnershipChanges = preview.OwnershipChanges
			}
			report.Status = "valid"
			return writeTargetDryRunReport(cmd, report, nil)
		},
	}
	cmd.Flags().StringVar(&profilePath, "profile", "", "Path to the local target profile")
	cmd.Flags().StringVar(&sourcePath, "source", "", "Path to the local source directory")
	cmd.Flags().StringVar(&importPath, "import", "", "Path to the target-import/v1 manifest")
	cmd.Flags().StringVar(&destinationPath, "destination", "", "Existing admitted target root to preview")
	_ = cmd.MarkFlagRequired("profile")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("import")
	return cmd
}

func writeTargetDryRunReport(cmd *cobra.Command, report targetDryRunReport, resultErr error) error {
	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
		return fmt.Errorf("encode target dry-run report: %w", err)
	}
	return resultErr
}

func readTargetDryRunInput(path string, limit int64) ([]byte, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve input path: %w", err)
	}
	parent, err := fsboundary.OpenDir(filepath.Dir(absolute))
	if err != nil {
		return nil, fmt.Errorf("open input parent: %w", err)
	}
	defer func() { _ = parent.Close() }()
	data, identity, err := parent.ReadFile(filepath.Base(absolute), limit)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	if !identity.Mode.IsRegular() {
		return nil, errors.New("input is not a regular file")
	}
	return data, nil
}

func targetDryRunRequirementRef(requirement yaml.Node) string {
	if requirement.Kind == yaml.ScalarNode {
		return requirement.Value
	}
	values := make(map[string]string, len(requirement.Content)/2)
	for i := 0; i+1 < len(requirement.Content); i += 2 {
		values[requirement.Content[i].Value] = requirement.Content[i+1].Value
	}
	ref := values["kind"] + ":" + values["name"]
	var qualifiers []string
	for _, field := range []string{"registry", "path"} {
		if value := values[field]; value != "" {
			qualifiers = append(qualifiers, fmt.Sprintf("%s=%q", field, value))
		}
	}
	if len(qualifiers) != 0 {
		ref += " [" + strings.Join(qualifiers, ", ") + "]"
	}
	return ref
}

func admitTargetDryRunDestination(destination string, imported targetprofile.ImportedPackage) (string, string, error) {
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return "", "", errors.New("destination must be an absolute clean path")
	}
	platform := ""
	switch runtime.GOOS {
	case "darwin":
		platform = "macos"
	case "linux":
		platform = "linux"
	default:
		return "", "", errors.New("unsupported host platform")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return "", "", err
	}
	stateRoot := filepath.Dir(stateDir)
	environment := targetprofile.ApprovalEnvironment{
		Platform: platform, Home: home, XDGConfig: os.Getenv("XDG_CONFIG_HOME"),
		CanonicalStateRoot: stateRoot,
	}
	if override, ok := os.LookupEnv(imported.Profile.Roots.User.Override); ok {
		environment.Override = &override
	}
	admitted, _, err := targetprofile.ResolvePlaceRoot(imported.Profile, imported.Identity, environment)
	if err != nil {
		return "", "", err
	}
	root, err := fsboundary.OpenCanonicalDir(destination)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = root.Close() }()
	if root.Path() != admitted {
		return "", "", errors.New("destination is not the profile's admitted root")
	}
	return root.Path(), stateRoot, nil
}
