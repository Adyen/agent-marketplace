package localapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
)

type configurationDTO struct {
	UserTargets         []string `json:"userTargets"`
	OrganizationTargets []string `json:"organizationTargets"`
	SupportedTargets    []string `json:"supportedTargets"`
	RootPresent         bool     `json:"rootPresent"`
	Editable            bool     `json:"editable"`
	ManagedOutsideGUI   []string `json:"managedOutsideGUI"`
}

func (a *API) userConfigPath() string {
	return filepath.Join(a.homeDir, ".agents", "config.yaml")
}

func (a *API) configurationMetadata(ctx context.Context) configurationDTO {
	dto := configurationDTO{
		UserTargets:         []string{},
		OrganizationTargets: []string{},
		SupportedTargets:    config.SupportedHarnessTargets(),
		ManagedOutsideGUI:   []string{},
	}
	if _, err := os.Lstat(a.userConfigPath()); err == nil {
		dto.RootPresent = true
	}
	intent := config.ReadIntent(ctx, config.LoadEffectiveOptions{
		RepoRoot:       a.homeDir,
		LocalConfigDir: a.homeDir,
		GlobalPath:     a.userConfigPath(),
		RepoPath:       config.ProjectConfigPath(a.homeDir),
		LocalPath:      config.LocalConfigPath(a.homeDir),
	})
	user := intent.Layers[config.UserPersonal]
	dto.Editable = dto.RootPresent && user.Present() && user.Err() == nil && len(intent.Errors) == 0

	dto.UserTargets = append(dto.UserTargets, user.Settings().Harnesses...)
	dto.OrganizationTargets = append(dto.OrganizationTargets, intent.Layers[config.Organization].Settings().Harnesses...)

	if len(user.Reads) > 1 {
		dto.ManagedOutsideGUI = append(dto.ManagedOutsideGUI, "included files")
	}
	if len(user.Sources()) > 0 || len(user.LegacyMarketplaces()) > 0 {
		dto.ManagedOutsideGUI = append(dto.ManagedOutsideGUI, "per-source configuration")
	}
	settings := user.Settings()
	if settings.Backup != nil || settings.Logging != nil || settings.Telemetry != nil ||
		settings.Materialization != nil || settings.AllowedRegistries != nil ||
		settings.AllowedMarketplaces != nil {
		dto.ManagedOutsideGUI = append(dto.ManagedOutsideGUI, "other settings")
	}
	return dto
}

func (a *API) handleConfiguration(w http.ResponseWriter, r *http.Request) {
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	if data, err := io.ReadAll(r.Body); err != nil || len(data) != 0 {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a.configurationMetadata(r.Context()))
}

func parseConfigurationAction(r *http.Request) ([]string, bool) {
	var action string
	var targets []string
	seenAction, seenTargets := false, false
	if !decodeStrictObject(r, func(decoder *json.Decoder, key string) bool {
		switch key {
		case "action":
			if seenAction {
				return false
			}
			seenAction = true
			return decoder.Decode(&action) == nil && action != ""
		case "targets":
			if seenTargets {
				return false
			}
			seenTargets = true
			return decoder.Decode(&targets) == nil
		default:
			return false
		}
	}) {
		return nil, false
	}
	if action != "save-targets" || len(targets) == 0 {
		return nil, false
	}
	for i, target := range targets {
		targets[i] = strings.TrimSpace(target)
		if targets[i] == "" {
			return nil, false
		}
	}
	return targets, true
}

func (a *API) handleConfigurationAction(w http.ResponseWriter, r *http.Request) {
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	targets, ok := parseConfigurationAction(r)
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}

	operation, ok := a.gate.Begin(session.OperationTargets)
	if !ok {
		writeError(w, http.StatusConflict, codeBusy)
		return
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		a.gate.Finish(operation)
		writeError(w, http.StatusServiceUnavailable, codeClosed)
		return
	}
	ready := a.current != nil && a.current.Stage != "provisional" && a.current.Stage != "config"
	metadata := a.configurationMetadata(r.Context())
	a.mu.Unlock()
	if !ready || !metadata.Editable {
		a.gate.Finish(operation)
		writeError(w, http.StatusBadRequest, codeNotReady)
		return
	}
	existing := make(map[string]bool, len(metadata.UserTargets))
	for _, target := range metadata.UserTargets {
		existing[target] = true
	}
	for _, target := range targets {
		if !config.IsSupportedHarness(target) && !existing[target] {
			a.gate.Finish(operation)
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
	}
	a.wg.Add(1)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"operation": operationRef{ID: uint64(operation), Kind: string(session.OperationTargets)}})

	go a.runOperation(operation, session.OperationTargets, func(ctx context.Context, _ session.RequestGeneration) (warnings []error, final session.Snapshot, failed error) {
		err := config.SetHarnessesInConfig(a.userConfigPath(), targets)
		final = a.readSnapshot(ctx, session.CatalogLoadCached)
		return nil, final, err
	})
}
