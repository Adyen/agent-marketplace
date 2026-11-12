package session

// RequestKind identifies a logical background request channel. Generations
// isolate overlapping requests so a superseded delivery cannot overwrite
// newer state.
type RequestKind string

// RequestGeneration identifies one issuance of a request.
type RequestGeneration uint64

const (
	RequestDetail         RequestKind = "detail"
	RequestSnapshot       RequestKind = "snapshot"
	RequestDiscovery      RequestKind = "marketplace-discovery"
	RequestInstallPreview RequestKind = "install-source-preview"
)

// RequestState tracks the newest issued generation per request kind. The
// fields are exported so host adapters and their fixtures can construct a
// specific state directly.
type RequestState struct {
	Next           RequestGeneration
	Detail         RequestGeneration
	Snapshot       RequestGeneration
	Discovery      RequestGeneration
	InstallPreview RequestGeneration
}

func (r *RequestState) Begin(kind RequestKind) RequestGeneration {
	switch kind {
	case RequestDetail, RequestSnapshot, RequestDiscovery, RequestInstallPreview:
	default:
		return 0
	}

	r.Next++
	switch kind {
	case RequestDetail:
		r.Detail = r.Next
	case RequestSnapshot:
		r.Snapshot = r.Next
	case RequestDiscovery:
		r.Discovery = r.Next
	case RequestInstallPreview:
		r.InstallPreview = r.Next
	}
	return r.Next
}

func (r RequestState) Active(kind RequestKind) RequestGeneration {
	switch kind {
	case RequestDetail:
		return r.Detail
	case RequestSnapshot:
		return r.Snapshot
	case RequestDiscovery:
		return r.Discovery
	case RequestInstallPreview:
		return r.InstallPreview
	default:
		return 0
	}
}

func (r *RequestState) Cancel(kind RequestKind) RequestGeneration {
	generation := r.Active(kind)
	switch kind {
	case RequestDetail:
		r.Detail = 0
	case RequestSnapshot:
		r.Snapshot = 0
	case RequestDiscovery:
		r.Discovery = 0
	case RequestInstallPreview:
		r.InstallPreview = 0
	}
	return generation
}

// OperationID identifies one guarded mutation operation.
type OperationID uint64

// activeOperation is the operation currently holding the serializer.
type activeOperation struct {
	ID   OperationID
	Kind OperationKind
}

// OperationState serializes guarded mutations: at most one operation holds
// the serializer at a time. Next is exported so host adapters and fixtures
// can construct a specific state directly.
type OperationState struct {
	Next   OperationID
	Active *activeOperation
}

func (s *OperationState) Begin(kind OperationKind) (OperationID, bool) {
	if s.Active != nil {
		return 0, false
	}
	s.Next++
	s.Active = &activeOperation{ID: s.Next, Kind: kind}
	return s.Next, true
}

func (s OperationState) Accepts(id OperationID) bool {
	return id != 0 && s.Active != nil && s.Active.ID == id
}

func (s *OperationState) Finish(id OperationID) {
	if s.Accepts(id) {
		s.Active = nil
	}
}

// Issue allocates a fresh operation identity without taking the serializer.
// Phase observation for non-serialized operations (sync/update) uses these
// identities so their events can never be mistaken for another operation's.
func (s *OperationState) Issue() OperationID {
	s.Next++
	return s.Next
}
