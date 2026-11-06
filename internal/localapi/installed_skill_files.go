package localapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
)

type installedSkillFilesPayloadDTO struct {
	RowID              string              `json:"rowId"`
	Generation         uint64              `json:"generation"`
	Files              []inspectionFileDTO `json:"files"`
	PrimaryFileID      string              `json:"primaryFileId"`
	SelectedFileID     string              `json:"selectedFileId"`
	Content            string              `json:"content"`
	Truncated          bool                `json:"truncated"`
	UnsupportedMessage string              `json:"unsupportedMessage"`
}

func installedSkillFileID(rowID string, generation uint64, file session.InstalledSkillFile) string {
	identity, _ := json.Marshal([4]any{rowID, generation, file.Path, file.Identity})
	sum := sha256.Sum256(identity)
	return hex.EncodeToString(sum[:])
}

func (a *API) handleInstalledSkillFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
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
	if ctx.Err() != nil {
		return
	}

	row, generation, code := a.visibleResourceRow(request.rowID, session.MarketplaceInstalledSkillFiles)
	if code != "" {
		status := http.StatusBadRequest
		if code == codeClosed {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, code)
		return
	}
	bundle, err := session.OpenInstalledSkillBundle(row)
	if err != nil {
		a.installedSkillFilesFailure(w, r)
		return
	}
	defer func() { _ = bundle.Close() }()
	files := bundle.Files()
	response := installedSkillFilesPayloadDTO{
		RowID: request.rowID, Generation: uint64(generation),
		Files: make([]inspectionFileDTO, 0, len(files)),
	}
	fileByID := make(map[string]session.InstalledSkillFile, len(files))
	for _, file := range files {
		id := installedSkillFileID(request.rowID, uint64(generation), file)
		response.Files = append(response.Files, inspectionFileDTO{
			ID:              id,
			Path:            session.SanitizeTerminalData(file.Path, session.TerminalSingleLine),
			MediaType:       session.SanitizeTerminalData(file.MediaType, session.TerminalSingleLine),
			Size:            file.Size,
			InspectionClass: file.InspectionClass,
		})
		fileByID[id] = file
		if file.Path == bundle.PrimaryPath() {
			response.PrimaryFileID = id
		}
	}
	if response.PrimaryFileID == "" {
		a.installedSkillFilesFailure(w, r)
		return
	}
	response.SelectedFileID = response.PrimaryFileID
	if request.hasFileID {
		if _, found := fileByID[request.fileID]; !found {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		response.SelectedFileID = request.fileID
	}
	selected := fileByID[response.SelectedFileID]
	if selected.InspectionClass != inspection.ClassText {
		response.UnsupportedMessage = installedSkillUnsupportedMessage(selected)
	} else {
		data, err := bundle.ReadFile(selected.Path)
		if errors.Is(err, session.ErrInstalledSkillFileUnsupported) {
			response.UnsupportedMessage = installedSkillUnsupportedMessage(selected)
			for i := range response.Files {
				if response.Files[i].ID == response.SelectedFileID {
					response.Files[i].InspectionClass = inspection.ClassMetadata
					break
				}
			}
		} else if err != nil {
			if r.Context().Err() != nil {
				return
			}
			a.installedSkillFilesFailure(w, r)
			return
		} else {
			response.Content, response.Truncated = inspectionPreview(data)
		}
	}
	a.deliverInstalledSkillFiles(w, r, request.rowID, generation, response)
}

func (a *API) installedSkillFilesFailure(w http.ResponseWriter, r *http.Request) {
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
	writeError(w, http.StatusBadGateway, codeResourceFilesUnavailable)
}

func (a *API) deliverInstalledSkillFiles(w http.ResponseWriter, r *http.Request, rowID string, generation session.RequestGeneration, response installedSkillFilesPayloadDTO) {
	current, closed := a.visibleResourceRowStillCurrent(rowID, generation, session.MarketplaceInstalledSkillFiles)
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

func installedSkillUnsupportedMessage(file session.InstalledSkillFile) string {
	return unsupportedFileMessage(file.MediaType, file.Size)
}
