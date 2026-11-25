package localapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type sourcePreviewResource struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Problem     string `json:"problem,omitempty"`
}

type sourcePreviewResponse struct {
	Location  string                  `json:"location"`
	Ref       string                  `json:"ref"`
	Path      string                  `json:"path"`
	Commit    string                  `json:"commit"`
	Resources []sourcePreviewResource `json:"resources"`
	Bundles   []string                `json:"bundles"`
	Warnings  []string                `json:"warnings"`
	Failure   string                  `json:"failure,omitempty"`
}

// previewSource is a seam so tests can observe the preview without a network.
var previewSource = pipeline.PreviewSource

func parseSourcePreviewRequest(r *http.Request) (*configurationActionSource, bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, false
	}
	var source *configurationActionSource
	ok := decodeStrictObject(r, func(decoder *json.Decoder, key string) bool {
		if key != "source" || source != nil {
			return false
		}
		decoded, ok := decodeActionSource(decoder)
		source = decoded
		return ok && decoded != nil
	})
	if !ok || source == nil {
		return nil, false
	}
	return source, true
}

// handleSourcePreview loads a Git or directory source that is not configured
// yet and reports what it holds. It writes no configuration or lock.
func (a *API) handleSourcePreview(w http.ResponseWriter, r *http.Request) {
	if !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	source, ok := parseSourcePreviewRequest(r)
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	if source.Kind == string(config.V4SourceMarketplace) {
		writeError(w, http.StatusBadRequest, codeNotSupported)
		return
	}
	intent := a.configurationIntent(r.Context(), nil)
	if len(intent.Errors) != 0 {
		writeError(w, http.StatusBadRequest, codeNotReady)
		return
	}
	entry, valid := validateAddSource(source, sourceAllowlist(intent, source.Kind))
	if !valid {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}
	if entry.V4Kind == config.V4SourceDirectory && !directorySourceAvailable(entry, a.homeDir) {
		writeError(w, http.StatusBadRequest, codeDirectoryUnavailable)
		return
	}
	operation, ok := a.gate.Begin(session.OperationMutation)
	if !ok {
		writeError(w, http.StatusConflict, codeBusy)
		return
	}
	defer a.finishMutationGate(operation)

	ctx, cancel := context.WithCancel(a.ctx)
	stop := context.AfterFunc(r.Context(), cancel)
	defer func() {
		stop()
		cancel()
	}()

	preview, err := previewSource(ctx, a.workspace, entry.URL, entry.Ref, entry.Path)
	if r.Context().Err() != nil {
		return
	}
	response := sourcePreviewResponse{
		Location:  session.SanitizeTerminalData(preview.URL, session.TerminalSingleLine),
		Ref:       session.SanitizeTerminalData(preview.Ref, session.TerminalSingleLine),
		Path:      session.SanitizeTerminalData(preview.Path, session.TerminalSingleLine),
		Commit:    preview.Commit,
		Resources: sourcePreviewResources(preview),
		Bundles:   append([]string{}, preview.BundlePaths...),
		Warnings:  []string{},
	}
	// Configuration warnings are already shown in Diagnostics; keep the
	// preview to what concerns this source.
	configuration := map[string]bool{}
	for _, warning := range intent.Warnings {
		configuration[warning.Error()] = true
	}
	for _, warning := range preview.Warnings {
		if !configuration[warning.Error()] {
			response.Warnings = append(response.Warnings, session.SanitizeReturnedError(warning).Error())
		}
	}
	if err != nil {
		response.Failure = session.SanitizeReturnedError(err).Error()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func sourcePreviewResources(preview pipeline.SourcePreview) []sourcePreviewResource {
	resources := []sourcePreviewResource{}
	add := func(kind, name, description, problem string) {
		resources = append(resources, sourcePreviewResource{
			Kind:        kind,
			Name:        session.SanitizeTerminalData(name, session.TerminalSingleLine),
			Description: session.SanitizeTerminalData(description, session.TerminalSingleLine),
			Problem:     session.SanitizeTerminalData(problem, session.TerminalSingleLine),
		})
	}
	for _, loaded := range preview.Catalogs {
		for _, entry := range loaded.Entries {
			add(entry.Kind.String(), entry.Name, entry.Description, "")
		}
		for _, malformed := range loaded.Malformed {
			name := malformed.DirName
			if name == "" {
				name = malformed.DeclaredName
			}
			kind := malformed.Kind
			if kind == "" {
				kind = ir.KindSkill.String()
			}
			problem := "This resource could not be read."
			if malformed.Error != nil {
				problem = malformed.Error.Error()
			}
			add(kind, name, "", problem)
		}
	}
	for _, col := range preview.Collections {
		add(ir.KindCollection.String(), col.Name, col.Description, "")
	}
	sort.SliceStable(resources, func(i, j int) bool {
		if resources[i].Kind != resources[j].Kind {
			return resources[i].Kind < resources[j].Kind
		}
		return resources[i].Name < resources[j].Name
	})
	return resources
}
