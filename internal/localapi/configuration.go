package localapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/giturl"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/redact"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

type sourceDescriptionDTO struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Location      string `json:"location"`
	Ref           string `json:"ref"`
	Path          string `json:"path"`
	SourceHarness string `json:"sourceHarness"`
}

func (a *API) sourceID(scope string, desc config.SourceDescription) string {
	mac := hmac.New(sha256.New, []byte(a.token))
	data, _ := json.Marshal([6]string{scope, string(desc.Kind), desc.Location, desc.Ref, desc.Path, desc.SourceHarness})
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *API) projectSources(scope string, descs []config.SourceDescription) []sourceDescriptionDTO {
	out := make([]sourceDescriptionDTO, 0, len(descs))
	for _, desc := range descs {
		out = append(out, sourceDescriptionDTO{
			ID:            a.sourceID(scope, desc),
			Kind:          string(desc.Kind),
			Location:      redact.URL(desc.Location),
			Ref:           desc.Ref,
			Path:          desc.Path,
			SourceHarness: desc.SourceHarness,
		})
	}
	return out
}

type configurationDTO struct {
	UserTargets          []string               `json:"userTargets"`
	OrganizationTargets  []string               `json:"organizationTargets"`
	UserSources          []sourceDescriptionDTO `json:"userSources"`
	OrganizationSources  []sourceDescriptionDTO `json:"organizationSources"`
	SupportedTargets     []string               `json:"supportedTargets"`
	RootPresent          bool                   `json:"rootPresent"`
	Editable             bool                   `json:"editable"`
	ManagedOutsideGUI    []string               `json:"managedOutsideGUI"`
	FirstRun             bool                   `json:"firstRun"`
	CanInitialize        bool                   `json:"canInitialize"`
	DetectedTargets      []string               `json:"detectedTargets"`
	DetectionUnavailable bool                   `json:"detectionUnavailable"`
	ConsentRequired      bool                   `json:"consentRequired"`
	TelemetryDestination string                 `json:"telemetryDestination"`
	RetentionPolicyURL   string                 `json:"retentionPolicyURL"`
}

func (a *API) userConfigPath() string {
	return filepath.Join(a.homeDir, ".agents", "config.yaml")
}

func (a *API) configurationIntent(ctx context.Context, overlays map[string][]byte) config.Intent {
	return config.ReadIntent(ctx, config.LoadEffectiveOptions{
		RepoRoot:       a.homeDir,
		LocalConfigDir: a.homeDir,
		GlobalPath:     a.userConfigPath(),
		RepoPath:       config.ProjectConfigPath(a.homeDir),
		LocalPath:      config.LocalConfigPath(a.homeDir),
		FileOverlays:   overlays,
	})
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
	intent := a.configurationIntent(ctx, nil)
	user := intent.Layers[config.UserPersonal]
	dto.Editable = dto.RootPresent && user.Present() && user.Err() == nil && len(intent.Errors) == 0

	dto.UserTargets = append(dto.UserTargets, user.Settings().Harnesses...)
	org := intent.Layers[config.Organization]
	dto.OrganizationTargets = append(dto.OrganizationTargets, org.Settings().Harnesses...)
	dto.UserSources = a.projectSources("user", user.DescribeSources())
	dto.OrganizationSources = a.projectSources("organization", org.DescribeSources())

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

type configurationActionSource struct {
	Kind     string
	Location string
	Ref      string
	Path     string
	refSeen  bool
	pathSeen bool
}

type configurationAction struct {
	action      string
	targets     []string
	consent     bool
	consentSeen bool
	source      *configurationActionSource
	id          string
	idSeen      bool
}

func decodeStrictString(decoder *json.Decoder, dst *string) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	s, ok := token.(string)
	if !ok {
		return false
	}
	*dst = s
	return true
}

func decodeActionSource(decoder *json.Decoder) (*configurationActionSource, bool) {
	source := &configurationActionSource{}
	seen := map[string]bool{}
	ok := decodeObject(decoder, func(decoder *json.Decoder, key string) bool {
		if seen[key] {
			return false
		}
		seen[key] = true
		var dst *string
		switch key {
		case "kind":
			dst = &source.Kind
		case "location":
			dst = &source.Location
		case "ref":
			source.refSeen = true
			dst = &source.Ref
		case "path":
			source.pathSeen = true
			dst = &source.Path
		default:
			return false
		}
		return decodeStrictString(decoder, dst)
	})
	if !ok {
		return nil, false
	}
	return source, true
}

func parseConfigurationAction(r *http.Request) (configurationAction, bool) {
	var req configurationAction
	seenAction, seenTargets, seenSource := false, false, false
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
		case "source":
			if seenSource {
				return false
			}
			seenSource = true
			source, ok := decodeActionSource(decoder)
			if !ok {
				return false
			}
			req.source = source
			return true
		case "id":
			if req.idSeen {
				return false
			}
			req.idSeen = true
			return decodeStrictString(decoder, &req.id)
		default:
			return false
		}
	}) {
		return configurationAction{}, false
	}
	switch req.action {
	case "save-targets":
		if req.consentSeen || req.source != nil || req.idSeen {
			return configurationAction{}, false
		}
	case "initialize":
		if req.source != nil || req.idSeen {
			return configurationAction{}, false
		}
	case "add-source":
		if req.source == nil || req.idSeen || seenTargets || req.consentSeen {
			return configurationAction{}, false
		}
	case "remove-source":
		if !req.idSeen || req.id == "" || req.source != nil || seenTargets || req.consentSeen {
			return configurationAction{}, false
		}
	default:
		return configurationAction{}, false
	}
	switch req.action {
	case "add-source", "remove-source":
		return req, true
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
	if req.action == "add-source" || req.action == "remove-source" {
		a.handleSourceMutation(w, r, req)
		return
	}
	a.handleSaveTargets(w, r, req.targets)
}

func sourceEntry(desc config.SourceDescription) config.RegistryEntry {
	return config.RegistryEntry{
		URL:           desc.Location,
		Ref:           desc.Ref,
		Path:          desc.Path,
		SourceHarness: desc.SourceHarness,
		V4Kind:        desc.Kind,
	}
}

func validGitLocation(location string) bool {
	if err := giturl.ValidateURL(location); err != nil {
		return false
	}
	if strings.Contains(location, "://") {
		u, err := url.Parse(location)
		if err != nil || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
		if u.User != nil {
			if _, hasPassword := u.User.Password(); hasPassword {
				return false
			}
			if u.Scheme != "ssh" {
				return false
			}
		}
		return true
	}
	if at := strings.IndexByte(location, '@'); at >= 0 && strings.Contains(location[:at], ":") {
		return false
	}
	return true
}

func validateAddSource(source *configurationActionSource, allowed []string) (config.RegistryEntry, bool) {
	kind := config.V4SourceKind(source.Kind)
	entry := config.RegistryEntry{
		V4Kind: kind,
		URL:    strings.TrimSpace(source.Location),
		Ref:    strings.TrimSpace(source.Ref),
		Path:   strings.TrimSpace(source.Path),
	}
	switch kind {
	case config.V4SourceDirectory:
		if entry.URL == "" || source.refSeen || source.pathSeen {
			return config.RegistryEntry{}, false
		}
		if !validDirectoryLocation(entry.URL) {
			return config.RegistryEntry{}, false
		}
	case config.V4SourceGit:
		if entry.URL == "" || entry.Ref == "" {
			return config.RegistryEntry{}, false
		}
		if err := giturl.ValidateRef(entry.Ref); err != nil {
			return config.RegistryEntry{}, false
		}
		if !validGitLocation(entry.URL) {
			return config.RegistryEntry{}, false
		}
		if err := config.ValidateRegistryPath(entry.Path); err != nil {
			return config.RegistryEntry{}, false
		}
	case config.V4SourceMarketplace:
		if entry.URL == "" || source.refSeen || source.pathSeen {
			return config.RegistryEntry{}, false
		}
		endpoint, err := config.NormalizeMarketplaceURL(entry.URL)
		if err != nil {
			return config.RegistryEntry{}, false
		}
		if err := config.ValidateMarketplaceURLs([]config.MarketplaceEntry{{URL: endpoint}}, allowed); err != nil {
			return config.RegistryEntry{}, false
		}
		entry.URL = endpoint
		return entry, true
	default:
		return config.RegistryEntry{}, false
	}
	if err := config.ValidateGlobalRegistryWrite(entry); err != nil {
		return config.RegistryEntry{}, false
	}
	if err := config.ValidateRegistryURLs([]config.RegistryEntry{entry}, allowed); err != nil {
		return config.RegistryEntry{}, false
	}
	return entry, true
}

func validDirectoryLocation(location string) bool {
	if filepath.IsAbs(location) || location == "~" || strings.HasPrefix(location, "~/") {
		return true
	}
	if !strings.HasPrefix(location, "file://") {
		return false
	}
	u, err := url.Parse(location)
	return err == nil && u.User == nil && u.Host == "" && filepath.IsAbs(u.Path) && u.RawQuery == "" && u.Fragment == ""
}

func (a *API) handleSourceMutation(w http.ResponseWriter, r *http.Request, req configurationAction) {
	home, err := os.UserHomeDir()
	if err != nil || home != a.homeDir {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	intent := a.configurationIntent(r.Context(), nil)
	user := intent.Layers[config.UserPersonal]
	if _, statErr := os.Lstat(a.userConfigPath()); statErr != nil ||
		!user.Present() || user.Err() != nil || len(intent.Errors) != 0 {
		writeError(w, http.StatusBadRequest, codeNotReady)
		return
	}

	marketplaceEndpoint := ""
	switch req.action {
	case "add-source":
		allowed := intent.AllowedRegistries()
		if req.source.Kind == string(config.V4SourceMarketplace) {
			allowed = intent.AllowedMarketplaces()
		}
		entry, valid := validateAddSource(req.source, allowed)
		if !valid {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		if entry.V4Kind == config.V4SourceMarketplace {
			marketplaceEndpoint = entry.URL
		}
	case "remove-source":
		matches := 0
		for _, desc := range user.DescribeSources() {
			if a.sourceID("user", desc) == req.id {
				matches++
				if desc.Kind != config.V4SourceGit && desc.Kind != config.V4SourceDirectory {
					matches++
				}
			}
		}
		if matches != 1 {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
	}

	operation, ok := a.gate.Begin(session.OperationMutation)
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
	a.wg.Add(1)
	a.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"operation": operationRef{ID: uint64(operation), Kind: string(session.OperationMutation)}})

	go a.runOperation(operation, session.OperationMutation, func(ctx context.Context, _ session.RequestGeneration) (warnings []error, final session.Snapshot, failed error) {
		if marketplaceEndpoint != "" {
			_, failed = pipeline.Install(ctx, pipeline.InstallOptions{
				RepoRoot:       a.homeDir,
				LocalConfigDir: a.homeDir,
				Args:           []string{marketplaceEndpoint},
				Scope:          pipeline.ScopeUser,
				ScopeSpecified: true,
			})
			final = a.readSnapshot(ctx, session.CatalogLoadCached)
			return nil, final, failed
		}
		failed = a.lifecycle.Mutate(ctx, a.workspace, func(mapper pipeline.ConfigPathMapper) error {
			staged, err := mapper(a.userConfigPath())
			if err != nil {
				return err
			}
			stagedBytes, err := os.ReadFile(staged)
			if err != nil {
				return err
			}
			stagedIntent := a.configurationIntent(ctx, map[string][]byte{a.userConfigPath(): stagedBytes})
			stagedUser := stagedIntent.Layers[config.UserPersonal]
			if !stagedUser.Present() || stagedUser.Err() != nil || len(stagedIntent.Errors) != 0 {
				return fmt.Errorf("%w: user configuration changed while staging", dependencytxn.ErrGenerationConflict)
			}
			var entry config.RegistryEntry
			switch req.action {
			case "add-source":
				var valid bool
				entry, valid = validateAddSource(req.source, stagedIntent.AllowedRegistries())
				if !valid {
					return fmt.Errorf("%w: source request is no longer valid under the staged configuration", dependencytxn.ErrGenerationConflict)
				}
			case "remove-source":
				var matches []config.SourceDescription
				for _, desc := range stagedUser.DescribeSources() {
					if a.sourceID("user", desc) == req.id {
						matches = append(matches, desc)
					}
				}
				if len(matches) != 1 ||
					(matches[0].Kind != config.V4SourceGit && matches[0].Kind != config.V4SourceDirectory) {
					return fmt.Errorf("%w: source descriptor changed while staging", dependencytxn.ErrGenerationConflict)
				}
				entry = sourceEntry(matches[0])
			}
			return pipeline.EditLayerAtPath(config.LayerTarget{
				Source:        config.UserPersonal,
				StagedPath:    staged,
				DeclaringPath: a.userConfigPath(),
			}, func(m *pipeline.LayerMutation) error {
				if req.action == "add-source" {
					_, err := m.EnsureRegistry(entry)
					return err
				}
				binding, found, err := m.LookupRegistry(entry)
				if err != nil {
					return err
				}
				if !found {
					return errors.New("source is no longer present in the user configuration")
				}
				return m.RemoveSource(binding)
			})
		})
		final = a.readSnapshot(ctx, session.CatalogLoadCached)
		return nil, final, failed
	})
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
