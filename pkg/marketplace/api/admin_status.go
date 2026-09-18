package api

import (
	"net/http"
	"strconv"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func (s *Server) handleAdminAttempts(w http.ResponseWriter, r *http.Request) {
	before, limit := int64(0), int64(50)
	var err error
	if value := r.URL.Query().Get("before"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
	}
	if err != nil || before < 0 {
		writeAdminError(w, http.StatusBadRequest, "invalid_pagination", "Invalid attempt cursor.")
		return
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.ParseInt(value, 10, 64)
	}
	if err != nil || limit < 1 || limit > 100 {
		writeAdminError(w, http.StatusBadRequest, "invalid_pagination", "Attempt limit must be between 1 and 100.")
		return
	}
	attempts, ok := s.meta.(store.ManagedRefreshAttemptStore)
	if !ok {
		writeAdminServiceError(w, store.ErrTopology)
		return
	}
	events, err := attempts.ListManagedRefreshAttempts(r.Context(), store.ManagedRefreshAttemptQuery{Marketplace: s.mpID, Registry: store.RegistryID(r.URL.Query().Get("registry_id")), JobID: store.JobID(r.URL.Query().Get("job_id")), Before: before, Limit: int(limit)})
	if err != nil {
		writeAdminServiceError(w, err)
		return
	}
	next := int64(0)
	if len(events) == int(limit) {
		next = events[len(events)-1].Sequence
	}
	if events == nil {
		events = []store.ManagedRefreshAttempt{}
	}
	writeAdminJSON(w, r, http.StatusOK, struct {
		Attempts   []store.ManagedRefreshAttempt `json:"attempts"`
		NextBefore int64                         `json:"next_before"`
	}{events, next})
}
