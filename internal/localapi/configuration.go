package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

type configurationDTO struct {
	UserTargets          []string `json:"userTargets"`
	OrganizationTargets  []string `json:"organizationTargets"`
	SupportedTargets     []string `json:"supportedTargets"`
	RootPresent          bool     `json:"rootPresent"`
	Editable             bool     `json:"editable"`
	ManagedOutsideGUI    []string `json:"managedOutsideGUI"`
	FirstRun             bool     `json:"firstRun"`
	CanInitialize        bool     `json:"canInitialize"`
	DetectedTargets      []string `json:"detectedTargets"`
	DetectionUnavailable bool     `json:"detectionUnavailable"`
	ConsentRequired      bool     `json:"consentRequired"`
	TelemetryDestination string   `json:"telemetryDestination"`
	RetentionPolicyURL   string   `json:"retentionPolicyURL"`
}

func (a *API) userConfigPath() string {
	return filepath.Join(a.homeDir, ".agents", "config.yaml")
}

func (a *API) configurationMetadata(ctx context.Context) configurationDTO {
	dto := configurationDTO{
		UserTargets:          []string{},
		OrganizationTargets:  []string{},
		SupportedTargets:     config.SupportedHarnessTargets(),
		ManagedOutsideGUI:    []string{},
		DetectedTargets:      []string{},
		TelemetryDestination: "not configured",
		RetentionPolicyURL:   "not configured",
	}
	if _, err := os.Lstat(a.userConfigPath()); err == nil {
		dto.RootPresent = true
	} else {
		dto.FirstRun = os.IsNotExist(err)
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
	org := intent.Layers[config.Organization]
	dto.OrganizationTargets = append(dto.OrganizationTargets, org.Settings().Harnesses...)

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

	homeReadable := false
	if info, err := os.Stat(a.homeDir); err == nil && info.IsDir() {
		if _, err := os.ReadDir(a.homeDir); err == nil {
			homeReadable = true
		}
	}
	dto.CanInitialize = dto.FirstRun && homeReadable && len(intent.Errors) == 0

	orgSettings := org.Settings()
	dto.ConsentRequired = orgSettings.Telemetry == nil || orgSettings.Telemetry.Level == ""

	if dto.FirstRun {
		detected, err := session.DetectHarnessTargets(a.homeDir)
		if err != nil {
			dto.DetectionUnavailable = true
		} else {
			installed := make(map[string]bool, len(detected))
			for _, id := range detected {
				installed[id] = true
			}
			for _, id := range dto.SupportedTargets {
				if installed[id] {
					dto.DetectedTargets = append(dto.DetectedTargets, id)
				}
			}
		}
	}

	if dto.ConsentRequired {
		if state, err := telemetry.ResolveTelemetryConfig(orgSettings, settings,
			telemetry.DefaultProjectEndpoint, telemetry.DefaultProjectRetentionURL); err == nil {
			if state.PrimaryEndpointHostname != "" {
				dto.TelemetryDestination = state.PrimaryEndpointHostname
			}
			if state.RetentionPolicyURL != "" {
				dto.RetentionPolicyURL = state.RetentionPolicyURL
			}
		}
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
	dto := a.configurationMetadata(r.Context())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dto)
}

type configurationAction struct {
	action      string
	targets     []string
	consent     bool
	consentSeen bool
}

func parseConfigurationAction(r *http.Request) (configurationAction, bool) {
	var req configurationAction
	seenAction, seenTargets := false, false
	if !decodeStrictObject(r, func(decoder *json.Decoder, key string) bool {
		switch key {
		case "action":
			if seenAction {
				return false
			}
			seenAction = true
			return decoder.Decode(&req.action) == nil && req.action != ""
		case "targets":
			if seenTargets {
				return false
			}
			seenTargets = true
			return decoder.Decode(&req.targets) == nil
		case "consent":
			if req.consentSeen {
				return false
			}
			req.consentSeen = true
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return false
			}
			switch string(bytes.TrimSpace(raw)) {
			case "true":
				req.consent = true
				return true
			case "false":
				return true
			default:
				return false
			}
		default:
			return false
		}
	}) {
		return configurationAction{}, false
	}
	switch req.action {
	case "save-targets":
		if req.consentSeen {
			return configurationAction{}, false
		}
	case "initialize":
	default:
		return configurationAction{}, false
	}
	if len(req.targets) == 0 {
		return configurationAction{}, false
	}
	for i, target := range req.targets {
		req.targets[i] = strings.TrimSpace(target)
		if req.targets[i] == "" {
			return configurationAction{}, false
		}
	}
	return req, true
}

func (a *API) handleConfigurationAction(w http.ResponseWriter, r *http.Request) {
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	req, ok := parseConfigurationAction(r)
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	if req.action == "initialize" {
		a.handleInitialize(w, r, req)
		return
	}
	a.handleSaveTargets(w, r, req.targets)
}

func (a *API) handleSaveTargets(w http.ResponseWriter, r *http.Request, targets []string) {
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

func (a *API) handleInitialize(w http.ResponseWriter, r *http.Request, req configurationAction) {
	operation, ok := a.gate.Begin(session.OperationMutation)
	if !ok {
		writeError(w, http.StatusConflict, codeBusy)
		return
	}
	reject := func(status int, code string) {
		a.gate.Finish(operation)
		writeError(w, status, code)
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		a.gate.Finish(operation)
		writeError(w, http.StatusServiceUnavailable, codeClosed)
		return
	}
	a.mu.Unlock()

	metadata := a.configurationMetadata(r.Context())
	if !metadata.FirstRun || !metadata.CanInitialize {
		reject(http.StatusBadRequest, codeNotReady)
		return
	}
	if metadata.ConsentRequired {
		if !req.consentSeen {
			reject(http.StatusBadRequest, codeInvalidRequest)
			return
		}
	} else if req.consentSeen {
		reject(http.StatusBadRequest, codeInvalidRequest)
		return
	}
	if err := config.ValidateHarnessIDs(req.targets); err != nil {
		reject(http.StatusBadRequest, codeInvalidRequest)
		return
	}
	a.wg.Add(1)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"operation": operationRef{ID: uint64(operation), Kind: string(session.OperationMutation)}})

	consent := req.consentSeen && req.consent
	go a.runOperation(operation, session.OperationMutation, func(ctx context.Context, _ session.RequestGeneration) (warnings []error, final session.Snapshot, failed error) {
		path, err := config.InitializeUserPersonalAtPath(a.userConfigPath(), req.targets)
		if err == nil && consent {
			err = config.SetUserTelemetryLevel(path, tmcfg.LevelBasic)
		}
		final = a.readSnapshot(ctx, session.CatalogLoadCached)
		return nil, final, err
	})
}
