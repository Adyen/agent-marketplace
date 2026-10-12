package localapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/Adyen/agent-marketplace/internal/telemetry"
)

const tokenHeader = "X-Agent-Marketplace-Token"

const guiOrigin = "wails://wails"

const (
	codeUnauthorized             = "localapi.unauthorized"
	codeForbidden                = "localapi.forbidden"
	codeInvalidRequest           = "localapi.invalid_request"
	codeNotReady                 = "localapi.not_ready"
	codeNotSupported             = "localapi.not_supported"
	codeBusy                     = "localapi.busy"
	codeClosed                   = "localapi.closed"
	codeInspectionUnavailable    = "localapi.inspection_unavailable"
	codeResourceFilesUnavailable = "localapi.resource_files_unavailable"
)

type operationRef struct {
	ID   uint64 `json:"id"`
	Kind string `json:"kind"`
}

type rowDTO struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	DisplayName         string   `json:"displayName"`
	SearchName          string   `json:"searchName"`
	SearchWords         []string `json:"searchWords"`
	Kind                string   `json:"kind"`
	Description         string   `json:"description"`
	Registry            string   `json:"registry"`
	Scope               string   `json:"scope"`
	Required            bool     `json:"required"`
	PendingVerification bool     `json:"pendingVerification"`
	Checked             bool     `json:"checked"`
	HarnessOverride     bool     `json:"harnessOverride"`
	Harnesses           []string `json:"harnesses"`
	Status              string   `json:"status"`
	Group               string   `json:"group"`
	Discovery           bool     `json:"discovery"`
	Inspectable         bool     `json:"inspectable"`
	InstalledFiles      bool     `json:"installedFiles"`
}

type diagnosticDTO struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type statusGroupDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type snapshotDTO struct {
	Stage              string               `json:"stage"`
	Drift              string               `json:"drift"`
	Rows               []rowDTO             `json:"rows"`
	Groups             []statusGroupDTO     `json:"groups"`
	Diagnostics        []diagnosticDTO      `json:"diagnostics"`
	Harnesses          []string             `json:"harnesses"`
	PendingChanges     int                  `json:"pendingChanges"`
	MigrationOffered   bool                 `json:"migrationOffered"`
	DiscoveryEnabled   bool                 `json:"discoveryEnabled"`
	DiscoveryHealth    []discoveryHealthDTO `json:"discoveryHealth"`
	DiscoveryQuery     string               `json:"discoveryQuery"`
	DiscoveryRequestID uint64               `json:"discoveryRequestId"`
	DiscoverySequence  uint64               `json:"discoverySequence"`
	DiscoveryLoading   bool                 `json:"discoveryLoading"`
	Operation          *operationRef        `json:"operation"`
	SettledOperation   *operationRef        `json:"settledOperation,omitempty"`
}

func statusGroupMetadata() []statusGroupDTO {
	groups := session.StatusGroups()
	metadata := make([]statusGroupDTO, 0, len(groups))
	for _, group := range groups {
		metadata = append(metadata, statusGroupDTO{ID: group.ID(), Title: group.Title()})
	}
	return metadata
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
	userOnly bool
	requests session.RequestState
	sequence uint64
	current  *snapshotDTO
	currentG session.RequestGeneration

	snapshotRowsKnown bool
	authoritativeRows []session.Row
	provisionalRows   []session.Row
	acceptedMergedCfg *config.Config
	acceptedConfigKey string
	discovery         discoveryState

	lastSettledOperation *operationRef
	started              bool
	diag                 session.DiagnosticStore
	subs                 map[*subscriber]struct{}

	version       string
	releaseClient *http.Client
	releaseOnce   sync.Once
	releaseNotice string

	wg sync.WaitGroup
}

func New(ctx context.Context, workspace pipeline.Workspace, homeDir string, lifecycle session.DependencyLifecycle, gate *session.OperationGate) (*API, error) {
	ws, err := session.ValidatedWorkspace(workspace)
	if err != nil {
		return nil, err
	}
	return commonNew(ctx, ws, homeDir, lifecycle, gate, false)
}

func NewUser(ctx context.Context, homeDir string, lifecycle session.DependencyLifecycle, gate *session.OperationGate, options ...UserOption) (*API, error) {
	if !config.IsHomeDir(homeDir) {
		return nil, fmt.Errorf("localapi: %s is not the user home directory", homeDir)
	}
	ws, err := pipeline.NewWorkspace(homeDir, homeDir)
	if err != nil {
		return nil, err
	}
	api, err := commonNew(ctx, ws, homeDir, lifecycle, gate, true)
	if err != nil {
		return nil, err
	}
	for _, option := range options {
		option(api)
	}
	return api, nil
}

func commonNew(ctx context.Context, ws pipeline.Workspace, homeDir string, lifecycle session.DependencyLifecycle, gate *session.OperationGate, userOnly bool) (*API, error) {
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
	if lifecycle.Mutate == nil {
		lifecycle.Mutate = runtime.Mutate
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
		userOnly:  userOnly,
		subs:      map[*subscriber]struct{}{},
		releaseClient: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (a *API) Credential() string { return a.token }

func (a *API) Close() {
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		a.cancel()
		a.cancelDiscoveryLocked()
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
	case "/api/discovery":
		a.handleDiscovery(w, r)
	case "/api/inspection":
		a.handleInspection(w, r)
	case "/api/installed-skill-files":
		a.handleInstalledSkillFiles(w, r)
	case "/api/actions":
		a.handleAction(w, r)
	case "/api/telemetry":
		a.handleTelemetry(w, r)
	case "/api/configuration":
		a.handleConfiguration(w, r)
	case "/api/configuration/actions":
		a.handleConfigurationAction(w, r)
	case "/api/release":
		a.handleRelease(w, r)
	default:
		writeError(w, http.StatusNotFound, codeInvalidRequest)
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	message := map[string]string{
		codeUnauthorized:             "unauthorized",
		codeForbidden:                "origin rejected",
		codeInvalidRequest:           "invalid request",
		codeNotReady:                 "session is not ready for mutation",
		codeNotSupported:             "operation not supported by this session mode",
		codeBusy:                     "another operation is in progress",
		codeClosed:                   "api is closed",
		codeInspectionUnavailable:    "Verified inspection is unavailable; check the Marketplace and retry.",
		codeResourceFilesUnavailable: "Resource files are unavailable; refresh this resource and retry.",
	}[code]
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

func (a *API) readSnapshotStages(ctx context.Context, mode session.CatalogLoadMode, generation session.RequestGeneration) <-chan session.Snapshot {
	if a.userOnly {
		return session.ReadUserSnapshotStages(ctx, a.homeDir, mode, generation)
	}
	return session.ReadSnapshotStagesForWorkspace(ctx, a.workspace, a.homeDir, mode, generation)
}

func (a *API) readSnapshot(ctx context.Context, mode session.CatalogLoadMode) session.Snapshot {
	if a.userOnly {
		return session.ReadUserSnapshot(ctx, a.homeDir, mode)
	}
	return session.ReadSnapshotForWorkspace(ctx, a.workspace, a.homeDir, mode)
}

func (a *API) userRootPresent() bool {
	info, err := os.Lstat(a.userConfigPath())
	return err == nil && !info.IsDir()
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
			if a.discovery.enabled && a.discovery.configKey == a.acceptedConfigKey {
				a.discovery.snapshotGeneration = generation
			}
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
	if a.closed || generation == 0 || a.requests.Active(session.RequestSnapshot) != generation || snapshot.Generation != generation {
		a.mu.Unlock()
		return nil
	}
	launch := a.acceptSnapshotStateLocked(snapshot)
	op := activeOperation(a.gate)
	a.diag.ReplaceSnapshot(snapshot.Diagnostics)
	dto := a.snapshotPresentationLocked(snapshot, op)
	a.emitLocked("snapshot", generation, dto.Operation, &dto)
	a.mu.Unlock()
	a.launchDiscovery(launch)
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
		for snapshot := range a.readSnapshotStages(ctx, session.CatalogLoadRefresh, generation) {
			if dto := a.deliverSnapshot(generation, snapshot); dto != nil {
				delivered = dto
			}
		}
	} else {
		snapshot := a.readSnapshot(ctx, session.CatalogLoadRefresh)
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
	health := append([]discoveryHealthDTO{}, a.discovery.health...)
	dto := snapshotDTO{
		Stage:              stage,
		Drift:              drift,
		Rows:               []rowDTO{},
		Groups:             statusGroupMetadata(),
		Diagnostics:        []diagnosticDTO{},
		Harnesses:          []string{},
		PendingChanges:     snapshot.DriftReport.PendingCount(),
		MigrationOffered:   snapshot.PendingMigration.HasConfigs(),
		DiscoveryEnabled:   a.discovery.enabled,
		DiscoveryHealth:    health,
		DiscoveryQuery:     a.discovery.query,
		DiscoveryRequestID: a.discovery.requestID,
		DiscoverySequence:  a.discovery.sequence,
		DiscoveryLoading:   a.discovery.loading,
		Operation:          op,
		SettledOperation:   a.lastSettledOperation,
	}
	var rows []session.Row
	if a.snapshotRowsKnown {
		rows = session.MergeDiscoveryRows(a.authoritativeRows, a.provisionalRows, a.discovery.rows)
	} else {
		switch {
		case stage == "provisional":
			rows = snapshot.ProvisionalRows
		case stage == "config" && a.current != nil:
			dto.Rows = append([]rowDTO{}, a.current.Rows...)
		default:
			rows = snapshot.Rows
		}
		if rows != nil {
			rows = session.MergeDiscoveryRows(rows, nil, a.discovery.rows)
		}
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
	row.EnsureSearchable()
	displayName := session.SanitizeTerminalData(row.Artifact.Name, session.TerminalSingleLine)
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
	registry = session.SanitizeTerminalData(registry, session.TerminalSingleLine)
	description := session.SanitizeTerminalData(row.Artifact.Description, session.TerminalMarkdownBody)
	status := session.SanitizeTerminalData(row.StatusText(), session.TerminalSingleLine)
	harnesses := append([]string{}, row.Harnesses...)
	for i, harness := range harnesses {
		harnesses[i] = session.SanitizeTerminalData(harness, session.TerminalSingleLine)
	}
	kind := row.Artifact.Kind.String()
	var identity []byte
	if discovery := row.MarketplaceDiscovery; discovery != nil {
		endpointURL := discovery.Endpoint.URL
		if normalized, err := marketplace.NormalizeMarketplaceURL(endpointURL); err == nil {
			endpointURL = normalized
		}
		identity, _ = json.Marshal([7]string{
			endpointURL,
			discovery.Endpoint.InstanceID,
			discovery.Item.RegistryID,
			kind,
			row.Artifact.Name,
			discovery.Item.Revision,
			discovery.Item.ContentDigest,
		})
	} else {
		identity, _ = json.Marshal([4]string{
			session.RegistryIdentityKey(row.RegistryURL, row.RegistryRef, row.RegistryPath),
			kind,
			row.Artifact.Name,
			scope,
		})
	}
	sum := sha256.Sum256(identity)
	return rowDTO{
		ID:                  kind + "/" + row.Artifact.Name + "#" + hex.EncodeToString(sum[:]),
		Name:                row.Artifact.Name,
		DisplayName:         displayName,
		SearchName:          row.SearchName,
		SearchWords:         append([]string{}, row.SearchWords...),
		Kind:                kind,
		Description:         description,
		Registry:            registry,
		Scope:               scope,
		Required:            row.Artifact.Required,
		PendingVerification: row.PendingVerification,
		Checked:             row.EnabledGlobal || row.EnabledProject || row.EnabledLocal || row.Artifact.Required,
		HarnessOverride:     row.HarnessSource == "registry override",
		Harnesses:           harnesses,
		Status:              status,
		Group:               row.StatusGroup().ID(),
		Discovery:           row.MarketplaceDiscovery != nil,
		Inspectable:         session.MarketplaceInspectableSkill(row),
		InstalledFiles:      session.MarketplaceInstalledSkillFiles(row),
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
	var startInitial session.RequestGeneration
	if a.userOnly && !a.started {
		a.started = true
		startInitial = a.beginSnapshotLocked()
	}
	a.mu.Unlock()
	if startInitial != 0 {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			for snapshot := range a.readSnapshotStages(a.ctx, session.CatalogLoadRefresh, startInitial) {
				a.deliverSnapshot(startInitial, snapshot)
			}
		}()
	}
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

func decodeObject(decoder *json.Decoder, field func(decoder *json.Decoder, key string) bool) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return false
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return false
		}
		key, ok := keyToken.(string)
		if !ok || !field(decoder, key) {
			return false
		}
	}
	token, err = decoder.Token()
	if err != nil {
		return false
	}
	if delim, ok := token.(json.Delim); !ok || delim != '}' {
		return false
	}
	return true
}

func decodeStrictObject(r *http.Request, field func(decoder *json.Decoder, key string) bool) bool {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	if !decodeObject(decoder, field) {
		return false
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return false
	}
	return true
}

func parseAction(r *http.Request) (session.OperationKind, bool) {
	var action string
	if !decodeStrictObject(r, func(decoder *json.Decoder, key string) bool {
		if key != "action" || action != "" {
			return false
		}
		return decoder.Decode(&action) == nil && action != ""
	}) {
		return "", false
	}
	switch action {
	case "sync":
		return session.OperationSync, true
	case "update":
		return session.OperationUpdate, true
	case "migrate":
		return session.OperationMigration, true
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
	if kind == session.OperationMigration && !a.userOnly {
		writeError(w, http.StatusBadRequest, codeNotSupported)
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
	ready := false
	if kind == session.OperationMigration {
		ready = session.PendingMigrationFor("", "", a.homeDir).HasConfigs()
	} else {
		current := a.current
		ready = current != nil && current.Stage != "provisional" && current.Stage != "config" && current.Drift != "failed"
		if ready && a.userOnly {
			ready = a.userRootPresent() && len(current.Harnesses) > 0
		}
		if ready {
			for _, d := range a.diag.Snapshot {
				if d.Severity == session.DiagnosticError {
					ready = false
					break
				}
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
	if kind == session.OperationSync || kind == session.OperationUpdate {
		telemetry.RecordInteractiveAction(a.ctx, "gui", string(kind), nil)
	}
	a.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"operation": operationRef{ID: uint64(operation), Kind: string(kind)}})

	go a.runOperation(operation, kind, func(ctx context.Context, generation session.RequestGeneration) (warnings []error, final session.Snapshot, failed error) {
		switch kind {
		case session.OperationSync:
			warnings, failed = session.RunDependencySync(ctx, a.workspace, a.lifecycle.Sync)
			pipeline.ObservePhase(ctx, pipeline.PhaseReload)
			final = a.readSnapshot(ctx, session.CatalogLoadCached)
		case session.OperationUpdate:
			result := session.RunDependencyUpdateWithLoader(ctx, a.workspace, a.homeDir, generation, a.lifecycle.Update, func(ctx context.Context, ws pipeline.Workspace, homeDir string, mode session.CatalogLoadMode) session.Snapshot {
				return a.readSnapshot(ctx, mode)
			})
			warnings, failed = result.Warnings, result.Err
			final = result.Snapshot
		case session.OperationMigration:
			prompt := session.PendingMigrationFor("", "", a.homeDir)
			if !prompt.HasConfigs() {
				failed = errors.New("no legacy user configuration remains to migrate")
			} else {
				_, failed = session.RunMigration(ctx, prompt)
			}
			final = a.readSnapshot(ctx, session.CatalogLoadCached)
		}
		return warnings, final, failed
	})
}

func (a *API) runOperation(operation session.OperationID, kind session.OperationKind, run func(ctx context.Context, generation session.RequestGeneration) (warnings []error, final session.Snapshot, failed error)) {
	defer a.wg.Done()
	op := &operationRef{ID: uint64(operation), Kind: string(kind)}

	a.mu.Lock()
	a.started = true
	generation := a.beginSnapshotLocked()
	var announcing snapshotDTO
	if a.current != nil {
		announcing = *a.current
	} else {
		announcing = a.snapshotPresentationLocked(session.Snapshot{Stage: session.SnapshotStageConfig}, op)
	}
	announcing.Operation = op
	a.emitLocked("snapshot", generation, op, &announcing)
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
	warnings, final, failed := run(ctx, generation)
	source.Close()
	<-done
	final.Generation = generation

	category := session.DiagnosticSync
	switch kind {
	case session.OperationUpdate:
		category = session.DiagnosticUpdate
	case session.OperationTargets, session.OperationMutation:
		category = session.DiagnosticMutation
	case session.OperationMigration:
		category = session.DiagnosticMigration
	}
	a.mu.Lock()
	a.diag.ReplaceSnapshot(final.Diagnostics)
	switch {
	case failed != nil:
		a.diag.RecordOperationFailure(kind, category, failed)
	case kind == session.OperationSync && len(warnings) > 0:
		a.diag.RecordOperationWarnings(kind, category, warnings...)
	default:
		a.diag.RecordOperationSuccess(kind)
	}
	a.lastSettledOperation = op
	launch := a.acceptSnapshotStateLocked(final)
	dto := a.snapshotPresentationLocked(final, nil)
	a.gate.Finish(operation)
	a.emitLocked("snapshot", generation, nil, &dto)
	severity, message := severityFor(warnings), string(kind)+" complete"
	if failed != nil {
		severity, message = "error", string(kind)+" failed"
	} else if kind != session.OperationTargets && kind != session.OperationMutation && (dto.Drift == "failed" || hasErrorDiagnostic(dto.Diagnostics)) {
		severity, message = "error", string(kind)+" failed"
	}
	a.emitLocked("notification", generation, op, notificationPayload{Severity: severity, Message: message})
	a.mu.Unlock()
	a.launchDiscovery(launch)
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
