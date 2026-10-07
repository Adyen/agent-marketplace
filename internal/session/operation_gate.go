package session

import "sync"

type OperationGate struct {
	mu    sync.Mutex
	state OperationState
}

func NewOperationGate() *OperationGate {
	return &OperationGate{}
}

func (g *OperationGate) Begin(kind OperationKind) (OperationID, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state.Begin(kind)
}

func (g *OperationGate) Finish(id OperationID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state.Finish(id)
}

func (g *OperationGate) Accepts(id OperationID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state.Accepts(id)
}

func (g *OperationGate) Active() (OperationID, OperationKind, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state.Active == nil {
		return 0, "", false
	}
	return g.state.Active.ID, g.state.Active.Kind, true
}
