package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func (s *Server) registerAdmin(mux *http.ServeMux) {
	mux.Handle("/api/v1/admin/", s.adminHandler())
}

func (s *Server) adminHandler() http.Handler {
	if !s.cfg.Admin.Enabled {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeAdminError(w, http.StatusNotFound, "not_found", "Administration is not enabled.")
		})
	}
	unavailable := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAdminError(w, http.StatusServiceUnavailable, "admin_unavailable", "Administration trust configuration is unavailable.")
	})
	var oidc *adminOIDCAuthenticator
	if s.cfg.Admin.AuthMode() == config.AdminAuthOIDC {
		var err error
		if oidc, err = newAdminOIDCAuthenticator(s.cfg.Admin); err != nil {
			return unavailable
		}
	}
	mux := http.NewServeMux()
	if oidc != nil {
		mux.Handle("GET "+adminAuthBase+"/login", http.HandlerFunc(oidc.handleLogin))
		mux.Handle("GET "+adminAuthBase+"/callback", http.HandlerFunc(oidc.handleCallback))
		mux.Handle("POST "+adminAuthBase+"/logout", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			adminAuthHeaders(w)
			if !adminRequestProtections(w, r, s.cfg.Admin) {
				return
			}
			oidc.handleLogout(w, r)
		}))
	}
	guard, err := newAdminGuardMode(s.cfg.Admin, oidc, http.HandlerFunc(s.handleAdmin))
	if err != nil {
		return unavailable
	}
	mux.Handle("/api/v1/admin/", guard)
	return mux
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Management.Mode != "database" {
		writeAdminError(w, http.StatusServiceUnavailable, "managed_configuration_required", "Administration requires adopted database configuration.")
		return
	}
	service := admin.Service{Metadata: s.meta, Deployment: s.cfg.Management}
	view, err := service.Read(r.Context(), s.mpID)
	if err != nil {
		writeAdminServiceError(w, err)
		return
	}
	route := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/")
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		s.handleAdminRead(w, r, service, view, route)
		return
	}
	if r.Method != http.MethodPost || (route != "changes" && route != "validate" && route != "previews") {
		writeAdminError(w, http.StatusMethodNotAllowed, "method_not_allowed", "This administration operation is not supported.")
		return
	}
	mutation, err := decodeAdminMutation(r)
	if err != nil {
		writeAdminServiceError(w, err)
		return
	}
	if route == "previews" {
		// A preview is a durable acquisition request, not a configuration
		// change: it has no idempotency contract and never advances revisions.
		mutation.Actor, _ = adminActor(r.Context())
		preview, err := service.Preview(r.Context(), s.mpID, mutation)
		if err != nil {
			writeAdminServiceError(w, err)
			return
		}
		writeAdminJSON(w, r, http.StatusAccepted, preview)
		return
	}
	if route == "validate" {
		if err := service.Validate(r.Context(), s.mpID, mutation); err != nil {
			writeAdminServiceError(w, err)
			return
		}
		writeAdminJSON(w, r, http.StatusOK, struct {
			Valid    bool     `json:"valid"`
			Warnings []string `json:"warnings"`
		}{true, []string{"Static validation only; repository acquisition has not been previewed."}})
		return
	}
	key, ok := singleAdminHeader(r, "Idempotency-Key")
	if !ok || !validIdentityValue(key) || len(key) > 256 {
		writeAdminError(w, http.StatusBadRequest, "idempotency_key_required", "A bounded idempotency key is required.")
		return
	}
	actor, _ := adminActor(r.Context())
	mutation.Actor, mutation.IdempotencyKey, mutation.RequestID = actor, key, rand.Text()
	accepted, err := service.Apply(r.Context(), s.mpID, mutation)
	if err != nil {
		writeAdminServiceError(w, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, accepted.Revision))
	writeAdminJSON(w, r, http.StatusAccepted, accepted)
}

func (s *Server) handleAdminRead(w http.ResponseWriter, r *http.Request, service admin.Service, view admin.View, route string) {
	switch route {
	case "session":
		actor, _ := adminActor(r.Context())
		writeAdminJSON(w, r, http.StatusOK, struct {
			Actor              store.ManagedActor `json:"actor"`
			Display            string             `json:"display,omitempty"`
			AuthenticationMode string             `json:"authentication_mode"`
			Capabilities       []string           `json:"capabilities"`
		}{actor, adminDisplay(r.Context()), s.cfg.Admin.AuthMode(), []string{"configuration", "lifecycle", "activity"}})
	case "configuration":
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, view.Revision))
		writeAdminJSON(w, r, http.StatusOK, view)
	case "attempts":
		s.handleAdminAttempts(w, r)
	case "activity":
		before, limit := int64(0), int64(50)
		var err error
		if value := r.URL.Query().Get("before"); value != "" {
			before, err = strconv.ParseInt(value, 10, 64)
		}
		if err != nil || before < 0 {
			writeAdminError(w, http.StatusBadRequest, "invalid_pagination", "Invalid activity cursor.")
			return
		}
		if value := r.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.ParseInt(value, 10, 64)
		}
		if err != nil || limit < 1 || limit > 100 {
			writeAdminError(w, http.StatusBadRequest, "invalid_pagination", "Activity limit must be between 1 and 100.")
			return
		}
		changes, err := service.History(r.Context(), s.mpID, before, int(limit))
		if err != nil {
			writeAdminServiceError(w, err)
			return
		}
		writeAdminJSON(w, r, http.StatusOK, struct {
			Changes []store.ManagedChange `json:"changes"`
		}{changes})
	case "profiles":
		type profile struct {
			ID             string   `json:"id"`
			AllowedOrigins []string `json:"allowed_origins"`
		}
		profiles := make([]profile, 0, len(s.cfg.Management.Profiles))
		for id, p := range s.cfg.Management.Profiles {
			profiles = append(profiles, profile{id, p.AllowedOrigins})
		}
		slices.SortFunc(profiles, func(a, b profile) int { return strings.Compare(a.ID, b.ID) })
		writeAdminJSON(w, r, http.StatusOK, struct {
			Profiles        []profile `json:"profiles"`
			SourceHarnesses []string  `json:"source_harnesses"`
		}{profiles, config.SourceHarnesses()})
	case "instance":
		writeAdminJSON(w, r, http.StatusOK, struct {
			Name           string `json:"name"`
			Version        string `json:"version"`
			ManagementMode string `json:"management_mode"`
			Revision       int64  `json:"revision"`
			WorkerStatus   string `json:"worker_status"`
		}{s.cfg.Instance.Name, s.version, s.cfg.Management.Mode, view.Revision, "Worker heartbeat is not available; inspect job activity."})
	default:
		if id, ok := strings.CutPrefix(route, "previews/"); ok && !strings.Contains(id, "/") {
			preview, err := service.GetPreview(r.Context(), s.mpID, id)
			if err != nil {
				writeAdminServiceError(w, err)
				return
			}
			writeAdminJSON(w, r, http.StatusOK, preview)
			return
		}
		if strings.HasPrefix(route, "jobs/") && !strings.Contains(strings.TrimPrefix(route, "jobs/"), "/") {
			job, err := s.meta.GetJob(r.Context(), store.JobID(strings.TrimPrefix(route, "jobs/")))
			if err != nil {
				writeAdminServiceError(w, err)
				return
			}
			var identity struct {
				Marketplace store.MarketplaceID `json:"marketplace_id"`
			}
			if !strings.HasPrefix(job.Kind, "managed-") || json.Unmarshal(job.Payload, &identity) != nil || identity.Marketplace != s.mpID {
				writeAdminError(w, http.StatusNotFound, "not_found", "Administration job not found.")
				return
			}
			writeAdminJSON(w, r, http.StatusOK, struct {
				ID          store.JobID     `json:"id"`
				Kind        string          `json:"kind"`
				Status      store.JobStatus `json:"status"`
				Attempt     int             `json:"attempt"`
				MaxAttempts int             `json:"max_attempts"`
				CreatedAt   string          `json:"created_at"`
				UpdatedAt   string          `json:"updated_at"`
			}{job.ID, job.Kind, job.Status, job.Attempt, job.MaxAttempts, job.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"), job.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")})
			return
		}
		writeAdminError(w, http.StatusNotFound, "not_found", "Administration endpoint not found.")
	}
}

var errAdminJSON = errors.New("invalid administration JSON")

func decodeAdminMutation(r *http.Request) (admin.Mutation, error) {
	version, ok := singleAdminHeader(r, "If-Match")
	if !ok {
		return admin.Mutation{}, admin.ErrRevisionRequired
	}
	if len(version) < 3 || version[0] != '"' || version[len(version)-1] != '"' {
		return admin.Mutation{}, store.ErrManagedChangeInvalid
	}
	revision, err := strconv.ParseInt(version[1:len(version)-1], 10, 64)
	if err != nil || revision <= 0 {
		return admin.Mutation{}, store.ErrManagedChangeInvalid
	}
	var body struct {
		Action              string               `json:"action"`
		RegistryID          store.RegistryID     `json:"registry_id"`
		Registry            *admin.Registry      `json:"registry"`
		LicensePolicy       *admin.LicensePolicy `json:"license_policy"`
		Reason              string               `json:"reason"`
		ConfirmOriginMove   bool                 `json:"confirm_origin_move"`
		ConfirmUnrestricted bool                 `json:"confirm_unrestricted"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return admin.Mutation{}, errAdminJSON
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return admin.Mutation{}, errAdminJSON
	}
	return admin.Mutation{Action: body.Action, RegistryID: body.RegistryID, Registry: body.Registry, LicensePolicy: body.LicensePolicy, Reason: body.Reason, ExpectedRevision: revision, ConfirmOriginMove: body.ConfirmOriginMove, ConfirmUnrestricted: body.ConfirmUnrestricted}, nil
}

func writeAdminJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(value)
	}
}

func writeAdminServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, admin.ErrRevisionRequired):
		writeAdminError(w, http.StatusPreconditionRequired, "precondition_required", "Read and review the current configuration before applying a change.")
	case errors.Is(err, store.ErrManagedIdentityConflict):
		writeAdminError(w, http.StatusConflict, "identity_conflict", "The registry identity or name is already reserved.")
	case errors.Is(err, store.ErrManagedRevisionConflict):
		writeAdminError(w, http.StatusConflict, "revision_conflict", "Configuration changed; reload and review your draft.")
	case errors.Is(err, store.ErrManagedIdempotencyConflict):
		writeAdminError(w, http.StatusConflict, "idempotency_conflict", "This idempotency key was used for a different change.")
	case errors.Is(err, admin.ErrInvalidConfiguration), errors.Is(err, store.ErrManagedChangeInvalid):
		// The validation detail names draft fields and operator-visible
		// deployment settings only, so it is safe to hand to the editor.
		message, fields := "The proposed configuration or operation is invalid.", map[string]string(nil)
		var invalid *admin.ValidationError
		if errors.As(err, &invalid) {
			message, fields = invalid.Message, invalid.Fields
		}
		writeAdminErrorFields(w, http.StatusUnprocessableEntity, "invalid_configuration", message, fields)
	case errors.Is(err, errAdminJSON):
		writeAdminError(w, http.StatusBadRequest, "invalid_json", "A single bounded JSON proposal with supported fields is required.")
	case errors.Is(err, store.ErrNotFound):
		writeAdminError(w, http.StatusNotFound, "not_found", "The requested administration record was not found.")
	default:
		writeAdminError(w, http.StatusServiceUnavailable, "persistence_unavailable", "Administration persistence is unavailable.")
	}
}
