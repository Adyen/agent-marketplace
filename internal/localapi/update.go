package localapi

import (
	"encoding/json"
	"net/http"

	"github.com/Adyen/agent-marketplace/internal/guiupdate"
)

func (a *API) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	action := ""
	seen := false
	if !decodeStrictObject(r, func(decoder *json.Decoder, key string) bool {
		if key != "action" || seen {
			return false
		}
		seen = true
		return decodeStrictString(decoder, &action)
	}) || !seen {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	if a.updater == nil && action != "status" {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	switch action {
	case "start":
		notice := a.currentReleaseNotice(r)
		if notice.LatestVersion == "" || notice.AssetURL == "" || notice.ChecksumsURL == "" || a.updater == nil {
			writeError(w, http.StatusBadRequest, codeNotSupported)
			return
		}
		writeUpdateStatus(w, http.StatusOK, a.updater.Start(guiupdate.Release{
			Version:        notice.LatestVersion,
			ReleasePageURL: notice.ReleasePageURL,
			AssetURL:       notice.AssetURL,
			ChecksumsURL:   notice.ChecksumsURL,
			DMGURL:         notice.DMGURL,
		}))
	case "status":
		status := guiupdate.Status{Phase: guiupdate.PhaseIdle}
		if a.updater != nil {
			status = a.updater.Status()
		}
		writeUpdateStatus(w, http.StatusOK, status)
	case "confirm":
		status, err := a.updater.Confirm()
		if err != nil && status.Phase != guiupdate.PhaseFailed {
			writeUpdateStatus(w, http.StatusConflict, status)
			return
		}
		writeUpdateStatus(w, http.StatusOK, status)
	case "cancel":
		writeUpdateStatus(w, http.StatusOK, a.updater.Cancel())
	default:
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
	}
}

func writeUpdateStatus(w http.ResponseWriter, statusCode int, status guiupdate.Status) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(status)
}
