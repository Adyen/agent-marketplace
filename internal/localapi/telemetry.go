package localapi

import (
	"encoding/json"
	"net/http"

	"github.com/Adyen/agent-marketplace/internal/telemetry"
)

type telemetryEvent struct {
	Action      string
	RowID       string
	Modal       string
	Outcome     string
	actionSeen  bool
	rowIDSeen   bool
	modalSeen   bool
	outcomeSeen bool
}

func parseTelemetryEvent(r *http.Request) (telemetryEvent, bool) {
	var event telemetryEvent
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return event, false
	}
	if !decodeStrictObject(r, func(decoder *json.Decoder, key string) bool {
		switch key {
		case "action":
			if event.actionSeen {
				return false
			}
			event.actionSeen = true
			return decodeStrictString(decoder, &event.Action)
		case "rowId":
			if event.rowIDSeen {
				return false
			}
			event.rowIDSeen = true
			return decodeStrictString(decoder, &event.RowID)
		case "modal":
			if event.modalSeen {
				return false
			}
			event.modalSeen = true
			return decodeStrictString(decoder, &event.Modal)
		case "outcome":
			if event.outcomeSeen {
				return false
			}
			event.outcomeSeen = true
			return decodeStrictString(decoder, &event.Outcome)
		default:
			return false
		}
	}) {
		return telemetryEvent{}, false
	}
	if !event.actionSeen {
		return telemetryEvent{}, false
	}
	switch event.Action {
	case "browse":
		return event, event.rowIDSeen && event.RowID != "" && !event.modalSeen && !event.outcomeSeen
	case "modal_outcome":
		if !event.modalSeen || !event.outcomeSeen || event.Outcome != "cancelled" {
			return telemetryEvent{}, false
		}
		switch event.Modal {
		case "sync+quit":
			return event, !event.rowIDSeen
		case "toggle":
			return event, event.rowIDSeen && event.RowID != ""
		default:
			return telemetryEvent{}, false
		}
	default:
		return telemetryEvent{}, false
	}
}

func (a *API) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	event, ok := parseTelemetryEvent(r)
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	switch event.Action {
	case "browse":
		kind, ok := a.currentTelemetryResourceKind(event.RowID)
		if !ok {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		telemetry.RecordInteractiveAction(a.ctx, "gui", "browse", map[string]string{"view": "detail", "resource.kind": kind})
	case "modal_outcome":
		attributes := map[string]string{"modal": event.Modal, "outcome": event.Outcome}
		if event.Modal == "toggle" {
			kind, ok := a.currentTelemetryResourceKind(event.RowID)
			if !ok {
				writeError(w, http.StatusBadRequest, codeInvalidRequest)
				return
			}
			attributes["resource.kind"] = kind
		}
		telemetry.RecordInteractiveAction(a.ctx, "gui", "modal_outcome", attributes)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) currentTelemetryResourceKind(rowID string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.current == nil {
		return "", false
	}
	kind := ""
	matches := 0
	for _, row := range a.current.Rows {
		if row.ID == rowID {
			kind = row.Kind
			matches++
		}
	}
	return kind, matches == 1
}
