package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

const codeInvalidSetting = "localapi.invalid_setting"
const codeSettingLocked = "localapi.setting_locked"

type settingFieldDTO struct {
	Value   any    `json:"value"`
	Default any    `json:"default"`
	Source  string `json:"source"`
	Locked  bool   `json:"locked"`
}

type backupSettingsDTO struct {
	Enabled   settingFieldDTO `json:"enabled"`
	MaxDays   settingFieldDTO `json:"maxDays"`
	MaxPerDay settingFieldDTO `json:"maxPerDay"`
}

type loggingSettingsDTO struct {
	Enabled    settingFieldDTO `json:"enabled"`
	Level      settingFieldDTO `json:"level"`
	File       settingFieldDTO `json:"file"`
	MaxSizeMB  settingFieldDTO `json:"maxSizeMB"`
	MaxBackups settingFieldDTO `json:"maxBackups"`
	MaxAgeDays settingFieldDTO `json:"maxAgeDays"`
	Compress   settingFieldDTO `json:"compress"`
}

type updateCheckSettingsDTO struct {
	Enabled settingFieldDTO `json:"enabled"`
}

type materializationSettingsDTO struct {
	Mode    settingFieldDTO `json:"mode"`
	Targets settingFieldDTO `json:"targets"`
}

type telemetrySettingsDTO struct {
	Level settingFieldDTO `json:"level"`
}

type settingsDTO struct {
	Backup          backupSettingsDTO          `json:"backup"`
	Logging         loggingSettingsDTO         `json:"logging"`
	UpdateCheck     updateCheckSettingsDTO     `json:"updateCheck"`
	Materialization materializationSettingsDTO `json:"materialization"`
	Telemetry       telemetrySettingsDTO       `json:"telemetry"`
}

// settingValue resolves one field: the user value wins, then the organization
// value (reported as the organization's default), then the built-in default.
func settingValue[T any](user, org *T, fallback T) settingFieldDTO {
	switch {
	case user != nil:
		return settingFieldDTO{Value: *user, Default: fallback, Source: "user"}
	case org != nil:
		return settingFieldDTO{Value: *org, Default: fallback, Source: "organization"}
	default:
		return settingFieldDTO{Value: fallback, Default: fallback, Source: "default"}
	}
}

// logFileSetting reports the log file path; with no path configured the
// value and default are both null, since the runtime picks its own location.
func logFileSetting(user, org *string) settingFieldDTO {
	field := settingValue(user, org, "")
	if field.Source == "default" {
		return settingFieldDTO{Value: nil, Default: nil, Source: "default"}
	}
	field.Default = nil
	return field
}

func nonEmptyString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nonEmptyTargets(targets map[string]string) *map[string]string {
	if len(targets) == 0 {
		return nil
	}
	return &targets
}

// settingsProjection reports effective user-personal settings. Only the
// organization's explicit telemetry level governs (and locks) its field.
func settingsProjection(user, org config.Settings) settingsDTO {
	var ub, ob config.BackupConfig
	if user.Backup != nil {
		ub = *user.Backup
	}
	if org.Backup != nil {
		ob = *org.Backup
	}
	var ul, ol config.LoggingConfig
	if user.Logging != nil {
		ul = *user.Logging
	}
	if org.Logging != nil {
		ol = *org.Logging
	}
	var uu, ou config.UpdateCheckConfig
	if user.UpdateCheck != nil {
		uu = *user.UpdateCheck
	}
	if org.UpdateCheck != nil {
		ou = *org.UpdateCheck
	}
	var um, om config.MaterializationConfig
	if user.Materialization != nil {
		um = *user.Materialization
	}
	if org.Materialization != nil {
		om = *org.Materialization
	}
	dto := settingsDTO{
		Backup: backupSettingsDTO{
			Enabled:   settingValue(ub.Enabled, ob.Enabled, true),
			MaxDays:   settingValue(ub.MaxDays, ob.MaxDays, 14),
			MaxPerDay: settingValue(ub.MaxPerDay, ob.MaxPerDay, 10),
		},
		Logging: loggingSettingsDTO{
			Enabled:    settingValue(ul.Enabled, ol.Enabled, config.DefaultLogEnabled),
			Level:      settingValue(ul.Level, ol.Level, config.DefaultLogLevel),
			File:       logFileSetting(ul.File, ol.File),
			MaxSizeMB:  settingValue(ul.MaxSizeMB, ol.MaxSizeMB, config.DefaultLogMaxSizeMB),
			MaxBackups: settingValue(ul.MaxBackups, ol.MaxBackups, config.DefaultLogMaxBackups),
			MaxAgeDays: settingValue(ul.MaxAgeDays, ol.MaxAgeDays, config.DefaultLogMaxAgeDays),
			Compress:   settingValue(ul.Compress, ol.Compress, config.DefaultLogCompress),
		},
		UpdateCheck: updateCheckSettingsDTO{Enabled: settingValue(uu.Enabled, ou.Enabled, true)},
		Materialization: materializationSettingsDTO{
			Mode:    settingValue(nonEmptyString(um.Mode), nonEmptyString(om.Mode), string(config.MaterializationModeSymlink)),
			Targets: settingValue(nonEmptyTargets(um.Targets), nonEmptyTargets(om.Targets), map[string]string{}),
		},
	}
	dto.Telemetry.Level = telemetryLevelSetting(user, org)
	return dto
}

func telemetryLevelSetting(user, org config.Settings) settingFieldDTO {
	if org.Telemetry != nil && strings.TrimSpace(org.Telemetry.Level) != "" {
		return settingFieldDTO{Value: org.Telemetry.Level, Default: nil, Source: "organization", Locked: true}
	}
	if user.Telemetry != nil && user.Telemetry.Level != "" {
		return settingFieldDTO{Value: user.Telemetry.Level, Default: nil, Source: "user"}
	}
	return settingFieldDTO{Value: nil, Default: nil, Source: "default"}
}

// optionalField distinguishes an absent key, an explicit null (clear), and a value.
type optionalField[T any] struct {
	set   bool
	null  bool
	value T
}

func (f *optionalField[T]) UnmarshalJSON(data []byte) error {
	f.set = true
	if string(bytes.TrimSpace(data)) == "null" {
		f.null = true
		return nil
	}
	return json.Unmarshal(data, &f.value)
}

func putSettingField[T any](fields map[string]any, key string, f optionalField[T]) {
	if !f.set {
		return
	}
	if f.null {
		fields[key] = nil
		return
	}
	fields[key] = f.value
}

type backupPatchRequest struct {
	Enabled   optionalField[bool] `json:"enabled"`
	MaxDays   optionalField[int]  `json:"maxDays"`
	MaxPerDay optionalField[int]  `json:"maxPerDay"`
}

type loggingPatchRequest struct {
	Enabled    optionalField[bool]   `json:"enabled"`
	Level      optionalField[string] `json:"level"`
	File       optionalField[string] `json:"file"`
	MaxSizeMB  optionalField[int]    `json:"maxSizeMB"`
	MaxBackups optionalField[int]    `json:"maxBackups"`
	MaxAgeDays optionalField[int]    `json:"maxAgeDays"`
	Compress   optionalField[bool]   `json:"compress"`
}

type updateCheckPatchRequest struct {
	Enabled optionalField[bool] `json:"enabled"`
}

type materializationPatchRequest struct {
	Mode    optionalField[string]            `json:"mode"`
	Targets optionalField[map[string]string] `json:"targets"`
}

type telemetryPatchRequest struct {
	Level optionalField[string] `json:"level"`
}

type saveSettingsRequest struct {
	Action          string                       `json:"action"`
	Backup          *backupPatchRequest          `json:"backup"`
	Logging         *loggingPatchRequest         `json:"logging"`
	UpdateCheck     *updateCheckPatchRequest     `json:"updateCheck"`
	Materialization *materializationPatchRequest `json:"materialization"`
	Telemetry       *telemetryPatchRequest       `json:"telemetry"`
}

// settingsPatch is one validated save: per-section field maps for the config
// editor, plus the telemetry level when it is part of the save.
type settingsPatch struct {
	sections       map[string]map[string]any
	telemetryLevel *string
}

func parseSaveSettings(body []byte) (settingsPatch, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var req saveSettingsRequest
	if err := decoder.Decode(&req); err != nil || req.Action != "save-settings" {
		return settingsPatch{}, false
	}
	patch := settingsPatch{sections: map[string]map[string]any{}}
	if req.Backup != nil {
		fields := map[string]any{}
		putSettingField(fields, "enabled", req.Backup.Enabled)
		putSettingField(fields, "max_days", req.Backup.MaxDays)
		putSettingField(fields, "max_per_day", req.Backup.MaxPerDay)
		patch.sections["backup"] = fields
	}
	if req.Logging != nil {
		fields := map[string]any{}
		putSettingField(fields, "enabled", req.Logging.Enabled)
		putSettingField(fields, "level", req.Logging.Level)
		putSettingField(fields, "file", req.Logging.File)
		putSettingField(fields, "max_size_mb", req.Logging.MaxSizeMB)
		putSettingField(fields, "max_backups", req.Logging.MaxBackups)
		putSettingField(fields, "max_age_days", req.Logging.MaxAgeDays)
		putSettingField(fields, "compress", req.Logging.Compress)
		patch.sections["logging"] = fields
	}
	if req.UpdateCheck != nil {
		fields := map[string]any{}
		putSettingField(fields, "enabled", req.UpdateCheck.Enabled)
		patch.sections["update_check"] = fields
	}
	if req.Materialization != nil {
		fields := map[string]any{}
		putSettingField(fields, "mode", req.Materialization.Mode)
		putSettingField(fields, "targets", req.Materialization.Targets)
		patch.sections["materialization"] = fields
	}
	if req.Telemetry != nil && req.Telemetry.Level.set {
		if req.Telemetry.Level.null {
			return settingsPatch{}, false
		}
		level := req.Telemetry.Level.value
		patch.telemetryLevel = &level
	}
	if len(patch.sections) == 0 && patch.telemetryLevel == nil {
		return settingsPatch{}, false
	}
	return patch, true
}

func (a *API) handleSaveSettings(w http.ResponseWriter, r *http.Request, body []byte) {
	patch, ok := parseSaveSettings(body)
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, codeClosed)
		return
	}
	ready := a.current != nil && a.current.Stage != "provisional" && a.current.Stage != "config"
	accepted := a.acceptedMergedCfg
	metadata := a.configurationMetadata(r.Context(), accepted)
	a.mu.Unlock()
	if !ready || !metadata.Editable {
		writeError(w, http.StatusBadRequest, codeNotReady)
		return
	}
	if patch.telemetryLevel != nil && metadata.Settings.Telemetry.Level.Locked {
		writeError(w, http.StatusConflict, codeSettingLocked)
		return
	}
	for section, fields := range patch.sections {
		if err := config.ValidateUserSettings(section, fields); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidSetting)
			return
		}
	}
	if patch.telemetryLevel != nil {
		if _, err := tmcfg.NormalizeLevel(*patch.telemetryLevel); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidSetting)
			return
		}
	}

	operation, ok := a.gate.Begin(session.OperationMutation)
	if !ok {
		writeError(w, http.StatusConflict, codeBusy)
		return
	}
	a.mu.Lock()
	a.invalidateUpdateAvailabilityLocked()
	a.wg.Add(1)
	a.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"operation": operationRef{ID: uint64(operation), Kind: string(session.OperationMutation)}})

	go a.runOperation(operation, session.OperationMutation, operationSubject{success: "Settings saved", failure: "Couldn't save settings"}, func(ctx context.Context, _ session.RequestGeneration) (warnings []error, final session.Snapshot, failed error) {
		failed = a.applySettingsPatch(patch)
		final = a.readSnapshot(ctx, session.CatalogLoadCached)
		return nil, final, failed
	})
}

func (a *API) applySettingsPatch(patch settingsPatch) error {
	path := a.userConfigPath()
	for section, fields := range patch.sections {
		if err := config.SetUserSettings(path, section, fields); err != nil {
			return err
		}
	}
	if patch.telemetryLevel != nil {
		if err := config.SetUserTelemetryLevel(path, *patch.telemetryLevel); err != nil {
			return fmt.Errorf("save telemetry level: %w", err)
		}
	}
	return nil
}
