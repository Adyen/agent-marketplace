package localapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type resourcePlanRequest struct {
	rowID string
}

type resourceActionRequest struct {
	rowID      string
	planID     string
	acceptRisk *bool
}

type resourceActionPlanResponse struct {
	RowID                  string `json:"rowId"`
	Generation             uint64 `json:"generation"`
	PlanID                 string `json:"planId"`
	Action                 string `json:"action"`
	Preview                string `json:"preview"`
	RequiresRiskAcceptance bool   `json:"requiresRiskAcceptance"`
}

type preparedResourceActionPlan struct {
	rowID                  string
	generation             session.RequestGeneration
	planID                 string
	action                 string
	row                    session.Row
	options                pipeline.InstallOptions
	prepared               *pipeline.PreparedMarketplaceInstall
	preview                string
	requiresRiskAcceptance bool
}

func (a *API) resourceRowPresentation(row session.Row) rowDTO {
	dto := rowPresentation(row)
	if a.userOnly {
		if action, err := session.UserMarketplaceAction(row); err == nil {
			dto.ResourceAction = action
		}
	}
	return dto
}

func (a *API) resourceActionsReadyLocked() bool {
	current := a.current
	if !a.userOnly || a.closed || !a.snapshotRowsKnown || current == nil || a.currentG == 0 {
		return false
	}
	if current.Stage == "provisional" || current.Stage == "config" || current.Drift == "failed" || !a.userRootPresent() || len(current.Harnesses) == 0 {
		return false
	}
	for _, diagnostic := range a.diag.Snapshot {
		if diagnostic.Severity == session.DiagnosticError {
			return false
		}
	}
	return true
}

func (a *API) currentUserResourceActionRowLocked(rowID string) (session.Row, session.RequestGeneration, string, string) {
	if a.closed {
		return session.Row{}, 0, "", codeClosed
	}
	if !a.userOnly {
		return session.Row{}, 0, "", codeNotSupported
	}
	if !a.resourceActionsReadyLocked() {
		return session.Row{}, 0, "", codeNotReady
	}
	rows := session.MergeDiscoveryRows(a.authoritativeRows, a.provisionalRows, a.discovery.rows)
	for _, row := range rows {
		if rowPresentation(row).ID != rowID {
			continue
		}
		action, err := session.UserMarketplaceAction(row)
		if err != nil {
			return session.Row{}, 0, "", codeNotSupported
		}
		if action == session.UserMarketplaceActionNone {
			return session.Row{}, 0, "", codeNotSupported
		}
		return cloneResourceActionRow(row), a.currentG, action, ""
	}
	return session.Row{}, 0, "", codeNotReady
}

func cloneResourceActionRow(row session.Row) session.Row {
	cloned := cloneInspectionRow(row)
	cloned.Harnesses = slices.Clone(row.Harnesses)
	cloned.MarketplaceSelections = maps.Clone(row.MarketplaceSelections)
	cloned.SearchWords = slices.Clone(row.SearchWords)
	cloned.SearchMatchRunes = slices.Clone(row.SearchMatchRunes)
	return cloned
}

func parseResourcePlanRequest(r *http.Request) (resourcePlanRequest, bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return resourcePlanRequest{}, false
	}
	var request resourcePlanRequest
	rowIDSeen := false
	ok := decodeStrictObject(r, func(decoder *json.Decoder, key string) bool {
		if key != "rowId" || rowIDSeen {
			return false
		}
		rowIDSeen = true
		return decodeStrictString(decoder, &request.rowID)
	})
	if !ok || !rowIDSeen || request.rowID == "" {
		return resourcePlanRequest{}, false
	}
	return request, true
}

func validResourceActionPlanID(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func parseResourceActionRequest(r *http.Request) (resourceActionRequest, bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return resourceActionRequest{}, false
	}
	var request resourceActionRequest
	rowIDSeen, planIDSeen, acceptRiskSeen := false, false, false
	ok := decodeStrictObject(r, func(decoder *json.Decoder, key string) bool {
		switch key {
		case "rowId":
			if rowIDSeen {
				return false
			}
			rowIDSeen = true
			return decodeStrictString(decoder, &request.rowID)
		case "planId":
			if planIDSeen {
				return false
			}
			planIDSeen = true
			return decodeStrictString(decoder, &request.planID)
		case "acceptRisk":
			if acceptRiskSeen {
				return false
			}
			acceptRiskSeen = true
			return decoder.Decode(&request.acceptRisk) == nil
		default:
			return false
		}
	})
	if !ok || !rowIDSeen || request.rowID == "" || !planIDSeen || !validResourceActionPlanID(request.planID) || !acceptRiskSeen || request.acceptRisk == nil {
		return resourceActionRequest{}, false
	}
	return request, true
}

func newResourceActionPlanID() (string, error) {
	var token [sha256.Size]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func resourceActionPreview(row session.Row) string {
	kind := session.SanitizeTerminalData(row.Artifact.Kind.String(), session.TerminalSingleLine)
	name := session.SanitizeTerminalData(row.Artifact.Name, session.TerminalSingleLine)
	return fmt.Sprintf("Resource: %s/%s\nLayer: user", kind, name)
}

func resourceActionLookupStatus(code string) int {
	if code == codeClosed {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadRequest
}

func (a *API) handleResourcePlan(w http.ResponseWriter, r *http.Request) {
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	request, ok := parseResourcePlanRequest(r)
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	operation, ok := a.gate.Begin(session.OperationMutation)
	if !ok {
		writeError(w, http.StatusConflict, codeBusy)
		return
	}
	defer a.gate.Finish(operation)

	ctx, cancel := context.WithCancel(a.ctx)
	stop := context.AfterFunc(r.Context(), cancel)
	defer func() {
		stop()
		cancel()
	}()
	if r.Context().Err() != nil {
		return
	}

	a.mu.Lock()
	row, generation, action, code := a.currentUserResourceActionRowLocked(request.rowID)
	if code != "" {
		a.mu.Unlock()
		writeError(w, resourceActionLookupStatus(code), code)
		return
	}
	a.resourceActionPlan = nil
	a.mu.Unlock()

	plan := &preparedResourceActionPlan{rowID: request.rowID, generation: generation, action: action, row: row, preview: resourceActionPreview(row)}
	if action == session.UserMarketplaceActionInstall {
		options, err := session.UserDiscoveryInstallOptions(a.homeDir, row)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeNotReady)
			return
		}
		plan.options = options
		sdkContext := pipeline.WithTransientOrganizationMarketplaceLockCache(ctx)
		prepared := session.PrepareDiscoveryInstall(sdkContext, options)
		if r.Context().Err() != nil {
			return
		}
		if a.ctx.Err() != nil {
			writeError(w, http.StatusServiceUnavailable, codeClosed)
			return
		}
		if prepared.Err != nil || prepared.Prepared == nil {
			writeError(w, http.StatusBadGateway, codeResourceActionUnavailable)
			return
		}
		plan.prepared = prepared.Prepared
		plan.requiresRiskAcceptance = prepared.Prepared.RequiresRiskAcceptance()
		plan.preview, _ = inspectionPreview([]byte(prepared.Preview))
	}
	planID, err := newResourceActionPlanID()
	if err != nil {
		writeError(w, http.StatusBadGateway, codeResourceActionUnavailable)
		return
	}
	if r.Context().Err() != nil {
		return
	}

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, codeClosed)
		return
	}
	currentRow, currentGeneration, currentAction, code := a.currentUserResourceActionRowLocked(request.rowID)
	if code != "" || currentGeneration != generation || currentAction != action || !reflect.DeepEqual(currentRow, row) {
		a.mu.Unlock()
		writeError(w, http.StatusBadRequest, codeNotReady)
		return
	}
	if r.Context().Err() != nil {
		a.mu.Unlock()
		return
	}
	plan.planID = planID
	a.resourceActionPlan = plan
	a.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resourceActionPlanResponse{
		RowID: plan.rowID, Generation: uint64(plan.generation), PlanID: plan.planID, Action: plan.action,
		Preview: plan.preview, RequiresRiskAcceptance: plan.requiresRiskAcceptance,
	})
}

func (a *API) handleResourceAction(w http.ResponseWriter, r *http.Request) {
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	request, ok := parseResourceActionRequest(r)
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	operation, ok := a.gate.Begin(session.OperationMutation)
	if !ok {
		writeError(w, http.StatusConflict, codeBusy)
		return
	}
	finishGate := func() { a.gate.Finish(operation) }

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		finishGate()
		writeError(w, http.StatusServiceUnavailable, codeClosed)
		return
	}
	plan := a.resourceActionPlan
	row, generation, action, code := a.currentUserResourceActionRowLocked(request.rowID)
	if code != "" {
		a.mu.Unlock()
		finishGate()
		writeError(w, resourceActionLookupStatus(code), code)
		return
	}
	if plan == nil || plan.planID != request.planID || plan.rowID != request.rowID {
		a.mu.Unlock()
		finishGate()
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	if plan.generation != generation || plan.action != action || !reflect.DeepEqual(plan.row, row) {
		a.resourceActionPlan = nil
		a.mu.Unlock()
		finishGate()
		writeError(w, http.StatusBadRequest, codeNotReady)
		return
	}
	if plan.requiresRiskAcceptance && !*request.acceptRisk {
		a.mu.Unlock()
		finishGate()
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	if r.Context().Err() != nil {
		a.resourceActionPlan = nil
		a.mu.Unlock()
		finishGate()
		return
	}
	acceptedPlan := *plan
	a.resourceActionPlan = nil
	a.wg.Add(1)
	a.mu.Unlock()

	if telemetryAction := resourceActionTelemetryName(acceptedPlan.action); telemetryAction != "" {
		telemetry.RecordInteractiveAction(a.ctx, "gui", telemetryAction, map[string]string{"resource.kind": acceptedPlan.row.Artifact.Kind.String()})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"operation": operationRef{ID: uint64(operation), Kind: string(session.OperationMutation)}})
	go a.runOperation(operation, session.OperationMutation, func(ctx context.Context, _ session.RequestGeneration) (warnings []error, final session.Snapshot, failed error) {
		ctx = pipeline.WithTransientOrganizationMarketplaceLockCache(ctx)
		switch acceptedPlan.action {
		case session.UserMarketplaceActionInstall:
			result := session.CommitDiscoveryInstall(ctx, acceptedPlan.options, acceptedPlan.prepared)
			failed = result.Err
			if result.Result != nil {
				warnings = append(warnings, result.Result.RequirementWarnings...)
			}
		case session.UserMarketplaceActionRemove:
			selection, found := acceptedPlan.row.MarketplaceSelections[config.UserPersonal]
			if !found {
				failed = fmt.Errorf("user Marketplace direct pin disappeared from the prepared row")
				break
			}
			failed = pipeline.ToggleMarketplacePin(ctx, a.homeDir, a.homeDir, a.userConfigPath(), config.UserPersonal, selection.Entry, selection.Pin, false)
		case session.UserMarketplaceActionExclude, session.UserMarketplaceActionRestore:
			selection, found := acceptedPlan.row.MarketplaceSelections[config.Organization]
			if !found || !acceptedPlan.row.CanOverrideMarketplace() || selection.Pin.RegistrySlug == "" || acceptedPlan.row.Artifact.Kind == ir.KindCollection {
				failed = fmt.Errorf("optional organization Marketplace selection is no longer eligible")
				break
			}
			userConfigPath := a.userConfigPath()
			failed = a.lifecycle.Mutate(ctx, a.workspace, func(mapper pipeline.ConfigPathMapper) error {
				mappedUserRoot, err := mapper(userConfigPath)
				if err != nil {
					return err
				}
				return config.SetUserMarketplaceExclusion(mappedUserRoot, selection.Entry.URL, selection.Pin.RegistrySlug, acceptedPlan.row.Artifact.Kind, selection.Pin.Name, acceptedPlan.action == session.UserMarketplaceActionExclude)
			})
		default:
			failed = fmt.Errorf("unsupported user Marketplace action")
		}
		final = a.readSnapshot(ctx, session.CatalogLoadCached)
		return warnings, final, failed
	})
}

func resourceActionTelemetryName(action string) string {
	switch action {
	case session.UserMarketplaceActionInstall, session.UserMarketplaceActionRestore:
		return "install"
	case session.UserMarketplaceActionRemove, session.UserMarketplaceActionExclude:
		return "remove"
	default:
		return ""
	}
}
