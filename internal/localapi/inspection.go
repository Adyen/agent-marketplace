package localapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
)

type inspectionRequest struct {
	rowID     string
	fileID    string
	hasFileID bool
}

type inspectionFileDTO struct {
	ID              string `json:"id"`
	Path            string `json:"path"`
	MediaType       string `json:"mediaType"`
	Size            int64  `json:"size"`
	InspectionClass string `json:"inspectionClass"`
}

type inspectionPayloadDTO struct {
	RowID              string              `json:"rowId"`
	Generation         uint64              `json:"generation"`
	Revision           string              `json:"revision"`
	ManifestDigest     string              `json:"manifestDigest"`
	Files              []inspectionFileDTO `json:"files"`
	PrimaryFileID      string              `json:"primaryFileId"`
	SelectedFileID     string              `json:"selectedFileId"`
	Content            string              `json:"content"`
	Truncated          bool                `json:"truncated"`
	UnsupportedMessage string              `json:"unsupportedMessage"`
}

func parseInspectionRequest(r *http.Request) (inspectionRequest, bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return inspectionRequest{}, false
	}
	var request inspectionRequest
	rowSeen, fileSeen := false, false
	ok := decodeStrictObject(r, func(decoder *json.Decoder, key string) bool {
		switch key {
		case "rowId":
			if rowSeen {
				return false
			}
			rowSeen = true
			return decodeStrictString(decoder, &request.rowID)
		case "fileId":
			if fileSeen {
				return false
			}
			fileSeen = true
			return decodeStrictString(decoder, &request.fileID)
		default:
			return false
		}
	})
	if !ok || !rowSeen || request.rowID == "" || (fileSeen && !validInspectionFileID(request.fileID)) {
		return inspectionRequest{}, false
	}
	request.hasFileID = fileSeen
	return request, true
}

func validInspectionFileID(value string) bool {
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

func inspectionMemberID(rowID string, file inspection.File) string {
	identity, _ := json.Marshal([3]string{rowID, file.Path, file.ContentDigest})
	sum := sha256.Sum256(identity)
	return hex.EncodeToString(sum[:])
}

func cloneInspectionRow(row session.Row) session.Row {
	cloned := row
	if discovery := row.MarketplaceDiscovery; discovery != nil {
		copy := *discovery
		copy.Endpoint.Resources = slices.Clone(discovery.Endpoint.Resources)
		copy.Item.Metadata = maps.Clone(discovery.Item.Metadata)
		cloned.MarketplaceDiscovery = &copy
	}
	return cloned
}

func (a *API) visibleResourceRow(rowID string, eligible func(session.Row) bool) (session.Row, session.RequestGeneration, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return session.Row{}, 0, codeClosed
	}
	if !a.userOnly {
		return session.Row{}, 0, codeNotSupported
	}
	if a.current == nil || a.currentG == 0 || !a.snapshotRowsKnown {
		return session.Row{}, 0, codeNotReady
	}
	rows := session.MergeDiscoveryRows(a.authoritativeRows, a.provisionalRows, a.discovery.rows)
	for _, row := range rows {
		if a.resourceRowPresentation(row).ID != rowID {
			continue
		}
		if !eligible(row) {
			return session.Row{}, 0, codeNotSupported
		}
		return cloneInspectionRow(row), a.currentG, ""
	}
	return session.Row{}, 0, codeNotReady
}

func (a *API) visibleResourceRowStillCurrent(rowID string, generation session.RequestGeneration, eligible func(session.Row) bool) (bool, bool) {
	_, currentGeneration, code := a.visibleResourceRow(rowID, eligible)
	if code == codeClosed {
		return false, true
	}
	return code == "" && currentGeneration == generation, false
}

func inspectionUnsupportedMessage(file inspection.File) string {
	return unsupportedFileMessage(file.MediaType, file.Size)
}

func unsupportedFileMessage(mediaType string, size int64) string {
	mediaType = session.SanitizeTerminalData(mediaType, session.TerminalSingleLine)
	if mediaType == "" {
		mediaType = "unknown"
	}
	return fmt.Sprintf("Preview is not supported for this file type (%s, %d bytes).", mediaType, size)
}

func truncateInspectionPreviewBytes(data []byte, limit int) []byte {
	if len(data) <= limit {
		return data
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(data[cut]) {
		cut--
	}
	return data[:cut]
}

func inspectionPreview(data []byte) (string, bool) {
	truncated := len(data) > session.MaxDetailSourceBytes
	if truncated {
		data = truncateInspectionPreviewBytes(data, session.MaxDetailSourceBytes)
	}
	content := session.SanitizeTerminalData(string(data), session.TerminalMarkdownBody)
	if len(content) > session.MaxDetailSourceBytes {
		content = string(truncateInspectionPreviewBytes([]byte(content), session.MaxDetailSourceBytes))
		truncated = true
	}
	if truncated {
		content += "\n\n> " + session.DetailTruncationMarker + "\n"
	}
	return content, truncated
}

func (a *API) inspectionFailure(w http.ResponseWriter, r *http.Request) {
	if r.Context().Err() != nil {
		return
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed || a.ctx.Err() != nil {
		writeError(w, http.StatusServiceUnavailable, codeClosed)
		return
	}
	writeError(w, http.StatusBadGateway, codeInspectionUnavailable)
}

func (a *API) handleInspection(w http.ResponseWriter, r *http.Request) {
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	request, ok := parseInspectionRequest(r)
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(a.ctx, cancel)
	defer func() {
		stop()
		cancel()
	}()

	row, generation, code := a.visibleResourceRow(request.rowID, session.MarketplaceInspectableSkill)
	if code != "" {
		status := http.StatusBadRequest
		if code == codeClosed {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, code)
		return
	}
	inspectionSource, err := session.OpenMarketplaceInspection(ctx, row)
	if err != nil {
		a.inspectionFailure(w, r)
		return
	}
	manifest := inspectionSource.Manifest()
	files := make([]inspectionFileDTO, 0, len(manifest.Files))
	fileByID := make(map[string]inspection.File, len(manifest.Files))
	primaryFileID := ""
	var primary inspection.File
	for _, file := range manifest.Files {
		id := inspectionMemberID(request.rowID, file)
		files = append(files, inspectionFileDTO{
			ID:              id,
			Path:            session.SanitizeTerminalData(file.Path, session.TerminalSingleLine),
			MediaType:       session.SanitizeTerminalData(file.MediaType, session.TerminalSingleLine),
			Size:            file.Size,
			InspectionClass: file.InspectionClass,
		})
		fileByID[id] = file
		if file.Path == manifest.PrimaryPath {
			primaryFileID = id
			primary = file
		}
	}
	if primaryFileID == "" {
		a.inspectionFailure(w, r)
		return
	}
	selectedID := primaryFileID
	selected := primary
	if request.hasFileID {
		var found bool
		selected, found = fileByID[request.fileID]
		if !found {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		selectedID = request.fileID
	}
	response := inspectionPayloadDTO{
		RowID:          request.rowID,
		Generation:     uint64(generation),
		Revision:       manifest.RevisionDigest,
		ManifestDigest: manifest.ManifestDigest,
		Files:          files,
		PrimaryFileID:  primaryFileID,
		SelectedFileID: selectedID,
	}
	if selected.InspectionClass != inspection.ClassText {
		response.UnsupportedMessage = inspectionUnsupportedMessage(selected)
	} else {
		data, err := inspectionSource.ReadFile(ctx, selected.Path)
		if err != nil {
			if r.Context().Err() != nil {
				return
			}
			a.inspectionFailure(w, r)
			return
		}
		response.Content, response.Truncated = inspectionPreview(data)
	}
	current, closed := a.visibleResourceRowStillCurrent(request.rowID, generation, session.MarketplaceInspectableSkill)
	if closed {
		writeError(w, http.StatusServiceUnavailable, codeClosed)
		return
	}
	if !current {
		writeError(w, http.StatusBadRequest, codeNotReady)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}
