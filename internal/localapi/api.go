package localapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
)

const tokenHeader = "X-Agent-Marketplace-Token"

const guiOrigin = "wails://wails"

const (
	codeUnauthorized   = "localapi.unauthorized"
	codeForbidden      = "localapi.forbidden"
	codeInvalidRequest = "localapi.invalid_request"
	codeNotReady       = "localapi.not_ready"
	codeBusy           = "localapi.busy"
	codeClosed         = "localapi.closed"
)

type operationRef struct {
	ID   uint64 `json:"id"`
	Kind string `json:"kind"`
}

type rowDTO struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Kind                string   `json:"kind"`
	Description         string   `json:"description"`
	Registry            string   `json:"registry"`
	Scope               string   `json:"scope"`
	Required            bool     `json:"required"`
	PendingVerification bool     `json:"pendingVerification"`
	Harnesses           []string `json:"harnesses"`
	Status              string   `json:"status"`
}

type diagnosticDTO struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type snapshotDTO struct {
	Stage       string          `json:"stage"`
	Drift       string          `json:"drift"`
	Rows        []rowDTO        `json:"rows"`
	Diagnostics []diagnosticDTO `json:"diagnostics"`
	Harnesses   []string        `json:"harnesses"`
	Operation   *operationRef   `json:"operation"`
}

type sessionEvent struct {
	Type       string        `json:"type"`
	Sequence   uint64        `json:"sequence"`
	Generation uint64        `json:"generation"`
	Operation  *operationRef `json:"operation"`
	Payload    any           `json:"payload"`
}

type progressPayload struct {
	ProgressSequence uint64 `json:"progressSequence"`
	Phase            string `json:"phase"`
}

type notificationPayload struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type subscriber struct {
	events chan []byte
}

type API struct {
	ctx       context.Context
	cancel    context.CancelFunc
	workspace pipeline.Workspace
	homeDir   string
	lifecycle session.DependencyLifecycle
	gate      *session.OperationGate
	token     string

	mu       sync.Mutex
	closed   bool
	requests session.RequestState
	sequence uint64
	current  *snapshotDTO
	currentG session.RequestGeneration
	started  bool
	diag     session.DiagnosticStore
	subs     map[*subscriber]struct{}

	wg sync.WaitGroup
}

func New(ctx context.Context, workspace pipeline.Workspace, homeDir string, lifecycle session.DependencyLifecycle, gate *session.OperationGate) (*API, error) {
	ws, err := session.ValidatedWorkspace(workspace)
	if err != nil {
		return nil, err
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	runtime := session.RuntimeLifecycle()
	if lifecycle.Sync == nil {
		lifecycle.Sync = runtime.Sync
	}
	if lifecycle.Update == nil {
		lifecycle.Update = runtime.Update
	}
	if gate == nil {
		gate = session.NewOperationGate()
	}
	apiCtx, cancel := context.WithCancel(ctx)
	return &API{
		ctx:       apiCtx,
		cancel:    cancel,
		workspace: ws,
		homeDir:   homeDir,
		lifecycle: lifecycle,
		gate:      gate,
		token:     hex.EncodeToString(token[:]),
		subs:      map[*subscriber]struct{}{},
	}, nil
}

func (a *API) Credential() string { return a.token }

func (a *API) Close() {
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		a.cancel()
	}
	a.mu.Unlock()
	a.wg.Wait()
}

func (a *API) admit() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	a.wg.Add(1)
	return true
}

func (a *API) Handler() http.Handler {
	return a.withAuth(false)
}

type Loopback struct {
	URL       string
	server    *http.Server
	cancel    context.CancelFunc
	serveDone chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func (a *API) ListenLoopback(ctx context.Context) (*Loopback, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, context.Canceled
	}
	a.wg.Add(1)
	a.mu.Unlock()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		a.wg.Done()
		return nil, err
	}
	watchCtx, cancel := context.WithCancel(ctx)
	lb := &Loopback{
		URL:       "http://" + ln.Addr().String(),
		cancel:    cancel,
		serveDone: make(chan struct{}),
		server: &http.Server{
			Handler:           a.withAuth(true),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
	go func() {
		defer close(lb.serveDone)
		if err := lb.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Debug("localapi loopback server ended", "err", err)
		}
	}()
	go func() {
		defer a.wg.Done()
		select {
		case <-watchCtx.Done():
		case <-a.ctx.Done():
		}
		_ = lb.server.Close()
		<-lb.serveDone
	}()
	return lb, nil
}

func (l *Loopback) Close() error {
	l.closeOnce.Do(func() {
		l.cancel()
		l.closeErr = l.server.Close()
		<-l.serveDone
	})
	return l.closeErr
}

func (a *API) withAuth(socket bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authorize(r) {
			writeError(w, http.StatusUnauthorized, codeUnauthorized)
			return
		}
		if socket {
			origin := r.Header.Values("Origin")
			if len(origin) != 1 || origin[0] != expectedOrigin(r) || r.Host != r.Context().Value(http.LocalAddrContextKey).(net.Addr).String() {
				writeError(w, http.StatusForbidden, codeForbidden)
				return
			}
		} else {
			origin := r.Header.Values("Origin")
			if len(origin) > 1 || (len(origin) == 1 && origin[0] != guiOrigin) {
				writeError(w, http.StatusForbidden, codeForbidden)
				return
			}
		}
		a.dispatch(w, r)
	})
}

func expectedOrigin(r *http.Request) string {
	return "http://" + r.Host
}

func (a *API) authorize(r *http.Request) bool {
	values := r.Header.Values(tokenHeader)
	if len(values) != 1 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(values[0]), []byte(a.token)) == 1
}

func (a *API) dispatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, codeInvalidRequest)
		return
	}
	if !a.admit() {
		writeError(w, http.StatusServiceUnavailable, codeClosed)
		return
	}
	defer a.wg.Done()
	switch r.URL.Path {
	case "/api/snapshot":
		a.handleSnapshot(w, r)
	case "/api/events":
		a.handleEvents(w, r)
	case "/api/actions":
		a.handleAction(w, r)
	default:
		writeError(w, http.StatusNotFound, codeInvalidRequest)
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	message := map[string]string{
		codeUnauthorized:   "unauthorized",
		codeForbidden:      "origin rejected",
		codeInvalidRequest: "invalid request",
		codeNotReady:       "session is not ready for mutation",
		codeBusy:           "another operation is in progress",
		codeClosed:         "api is closed",
	}[code]
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

func (a *API) beginSnapshotLocked() session.RequestGeneration {
	return a.requests.Begin(session.RequestSnapshot)
}

func (a *API) emitLocked(eventType string, generation session.RequestGeneration, op *operationRef, payload any) {
	a.sequence++
	event := sessionEvent{Type: eventType, Sequence: a.sequence, Generation: uint64(generation), Operation: op, Payload: payload}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	data = append(append([]byte("data: "), data...), '\n', '\n')
	if eventType == "snapshot" {
		if snapshot, ok := payload.(*snapshotDTO); ok {
			a.current = snapshot
			a.currentG = generation
		}
	}
	for sub := range a.subs {
		select {
		case sub.events <- data:
		default:
			delete(a.subs, sub)
			close(sub.events)
		}
	}
}

func (a *API) deliverSnapshot(generation session.RequestGeneration, snapshot session.Snapshot) *snapshotDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	if generation == 0 || a.requests.Active(session.RequestSnapshot) != generation || snapshot.Generation != generation {
		return nil
	}
	op := activeOperation(a.gate)
	a.diag.ReplaceSnapshot(snapshot.Diagnostics)
	dto := a.snapshotPresentationLocked(snapshot, op)
	a.emitLocked("snapshot", generation, dto.Operation, &dto)
	return &dto
}

func (a *API) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if operation := activeOperation(a.gate); operation != nil && a.current != nil {
		dto := *a.current
		dto.Operation = operation
		a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&dto)
		return
	}
	staged := !a.started
	a.started = true
	generation := a.beginSnapshotLocked()
	a.mu.Unlock()

	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()

	var delivered *snapshotDTO
	if staged {
		for snapshot := range session.ReadSnapshotStagesForWorkspace(ctx, a.workspace, a.homeDir, session.CatalogLoadRefresh, generation) {
			if dto := a.deliverSnapshot(generation, snapshot); dto != nil {
				delivered = dto
			}
		}
	} else {
		snapshot := session.ReadSnapshotForWorkspace(ctx, a.workspace, a.homeDir, session.CatalogLoadRefresh)
		snapshot.Generation = generation
		delivered = a.deliverSnapshot(generation, snapshot)
	}
	if delivered == nil {
		a.mu.Lock()
		delivered = a.current
		a.mu.Unlock()
	}
	if delivered == nil {
		writeError(w, http.StatusServiceUnavailable, codeNotReady)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(delivered)
}

func (a *API) snapshotPresentationLocked(snapshot session.Snapshot, op *operationRef) snapshotDTO {
	stage := "final"
	switch snapshot.Stage {
	case session.SnapshotStageConfig:
		if snapshot.ProvisionalCfg != nil {
			stage = "provisional"
		} else {
			stage = "config"
		}
	case session.SnapshotStageInventory:
		stage = "inventory"
	}
	drift := "pending"
	switch snapshot.DriftAssessment {
	case session.DriftReady:
		drift = "ready"
	case session.DriftFailed:
		drift = "failed"
	}
	dto := snapshotDTO{Stage: stage, Drift: drift, Rows: []rowDTO{}, Diagnostics: []diagnosticDTO{}, Harnesses: []string{}, Operation: op}
	var rows []session.Row
	switch {
	case stage == "provisional":
		rows = snapshot.ProvisionalRows
	case stage == "config" && a.current != nil:
		dto.Rows = a.current.Rows
	default:
		rows = snapshot.Rows
	}
	for _, row := range rows {
		dto.Rows = append(dto.Rows, rowPresentation(row))
	}
	for _, d := range a.diag.All() {
		if d.Err == nil {
			continue
		}
		severity := "warning"
		if d.Severity == session.DiagnosticError {
			severity = "error"
		}
		dto.Diagnostics = append(dto.Diagnostics, diagnosticDTO{
			Severity: severity,
			Category: string(d.Category),
			Code:     d.StableKey,
			Message:  session.SanitizeReturnedError(d.Err).Error(),
		})
	}
	for _, target := range snapshot.TargetHarnesses {
		dto.Harnesses = append(dto.Harnesses, string(target))
	}
	return dto
}

func activeOperation(gate *session.OperationGate) *operationRef {
	id, kind, ok := gate.Active()
	if !ok {
		return nil
	}
	return &operationRef{ID: uint64(id), Kind: string(kind)}
}

func rowPresentation(row session.Row) rowDTO {
	scope := "available"
	switch {
	case row.Artifact.Source == config.Organization:
		scope = "organization"
	case row.EnabledLocal || row.ExcludedLocally:
		scope = "local"
	case row.EnabledProject:
		scope = "project"
	case row.EnabledGlobal:
		scope = "user"
	}
	registry := row.RegistryName
	if registry == "" {
		registry = row.RegistryURL
	}
	harnesses := append([]string{}, row.Harnesses...)
	kind := row.Artifact.Kind.String()
	identity, _ := json.Marshal([4]string{
		session.RegistryIdentityKey(row.RegistryURL, row.RegistryRef, row.RegistryPath),
		kind,
		row.Artifact.Name,
		scope,
	})
	sum := sha256.Sum256(identity)
	return rowDTO{
		ID:                  kind + "/" + row.Artifact.Name + "#" + hex.EncodeToString(sum[:]),
		Name:                row.Artifact.Name,
		Kind:                kind,
		Description:         row.Artifact.Description,
		Registry:            registry,
		Scope:               scope,
		Required:            row.Artifact.Required,
		PendingVerification: row.PendingVerification,
		Harnesses:           harnesses,
		Status:              row.StatusText(),
	}
}

func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	sub := &subscriber{events: make(chan []byte, 64)}
	var envelope []byte
	a.mu.Lock()
	if a.current != nil {
		a.sequence++
		data, err := json.Marshal(sessionEvent{Type: "snapshot", Sequence: a.sequence, Generation: uint64(a.currentG), Operation: a.current.Operation, Payload: a.current})
		if err == nil {
			envelope = append(append([]byte("data: "), data...), '\n', '\n')
		}
	}
	a.subs[sub] = struct{}{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.subs, sub)
		a.mu.Unlock()
	}()

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	write := func(data []byte) bool {
		if _, err := w.Write(data); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	if envelope != nil && !write(envelope) {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.ctx.Done():
			return
		case data, ok := <-sub.events:
			if !ok || !write(data) {
				return
			}
		}
	}
}

func parseAction(r *http.Request) (session.OperationKind, bool) {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	token, err := decoder.Token()
	if err != nil {
		return "", false
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return "", false
	}
	var action string
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return "", false
		}
		key, ok := keyToken.(string)
		if !ok || key != "action" || action != "" {
			return "", false
		}
		if err := decoder.Decode(&action); err != nil || action == "" {
			return "", false
		}
	}
	token, err = decoder.Token()
	if err != nil {
		return "", false
	}
	if delim, ok := token.(json.Delim); !ok || delim != '}' {
		return "", false
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return "", false
	}
	switch action {
	case "sync":
		return session.OperationSync, true
	case "update":
		return session.OperationUpdate, true
	default:
		return "", false
	}
}

func (a *API) handleAction(w http.ResponseWriter, r *http.Request) {
	kind, ok := parseAction(r)
	if !ok {
		writeError(w, http.StatusBadRequest, codeInvalidRequest)
		return
	}

	operation, ok := a.gate.Begin(kind)
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
	current := a.current
	ready := current != nil && current.Stage != "provisional" && current.Stage != "config" && current.Drift != "failed"
	if ready {
		for _, d := range a.diag.Snapshot {
			if d.Severity == session.DiagnosticError {
				ready = false
				break
			}
		}
	}
	if !ready {
		a.mu.Unlock()
		a.gate.Finish(operation)
		writeError(w, http.StatusBadRequest, codeNotReady)
		return
	}
	a.wg.Add(1)
	a.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"operation": operationRef{ID: uint64(operation), Kind: string(kind)}})

	go a.runOperation(operation, kind)
}

func (a *API) runOperation(operation session.OperationID, kind session.OperationKind) {
	defer a.wg.Done()
	op := &operationRef{ID: uint64(operation), Kind: string(kind)}

	a.mu.Lock()
	generation := a.beginSnapshotLocked()
	if a.current != nil {
		announcing := *a.current
		announcing.Operation = op
		a.emitLocked("snapshot", generation, op, &announcing)
	}
	a.mu.Unlock()

	source := session.NewOperationProgressSource(operation, generation)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range source.Events() {
			if !source.Accept(event) {
				continue
			}
			a.mu.Lock()
			a.emitLocked("progress", event.Generation, op, progressPayload{ProgressSequence: event.Sequence, Phase: event.Phase})
			a.mu.Unlock()
		}
	}()

	ctx := pipeline.WithPhaseObserver(a.ctx, source.Observer())
	var warnings []error
	var failed error
	var final session.Snapshot
	switch kind {
	case session.OperationSync:
		warnings, failed = session.RunDependencySync(ctx, a.workspace, a.lifecycle.Sync)
		pipeline.ObservePhase(ctx, pipeline.PhaseReload)
		final = session.ReadSnapshotForWorkspace(ctx, a.workspace, a.homeDir, session.CatalogLoadCached)
	case session.OperationUpdate:
		result := session.RunDependencyUpdateWithLoader(ctx, a.workspace, a.homeDir, generation, a.lifecycle.Update, func(ctx context.Context, ws pipeline.Workspace, homeDir string, mode session.CatalogLoadMode) session.Snapshot {
			return session.ReadSnapshotForWorkspace(ctx, ws, homeDir, mode)
		})
		warnings, failed = result.Warnings, result.Err
		final = result.Snapshot
	}
	source.Close()
	<-done
	final.Generation = generation

	category := session.DiagnosticSync
	if kind == session.OperationUpdate {
		category = session.DiagnosticUpdate
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.diag.ReplaceSnapshot(final.Diagnostics)
	switch {
	case failed != nil:
		a.diag.RecordOperationFailure(kind, category, failed)
	case kind == session.OperationSync && len(warnings) > 0:
		a.diag.RecordOperationWarnings(kind, category, warnings...)
	default:
		a.diag.RecordOperationSuccess(kind)
	}
	dto := a.snapshotPresentationLocked(final, nil)
	a.gate.Finish(operation)
	a.emitLocked("snapshot", generation, nil, &dto)
	severity, message := severityFor(warnings), string(kind)+" complete"
	if failed != nil {
		severity, message = "error", string(kind)+" failed"
	} else if dto.Drift == "failed" || hasErrorDiagnostic(dto.Diagnostics) {
		severity, message = "error", string(kind)+" failed"
	}
	a.emitLocked("notification", generation, op, notificationPayload{Severity: severity, Message: message})
	if failed != nil {
		slog.Error("localapi operation failed", "kind", string(kind), "err", session.SanitizeReturnedError(failed))
	}
}

func hasErrorDiagnostic(diagnostics []diagnosticDTO) bool {
	for _, d := range diagnostics {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

func severityFor(warnings []error) string {
	if len(warnings) > 0 {
		return "warning"
	}
	return "info"
}
