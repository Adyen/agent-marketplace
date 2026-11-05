package tui

import (
	"context"
	"sync"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
)

// pendingInventory holds the latest accepted final snapshot of a session until
// the session ends. It is shared by pointer because Model is copied by value.
type pendingInventory struct {
	mu     sync.Mutex
	result *pipeline.ResolveResult
}

func newPendingInventory() *pendingInventory {
	return &pendingInventory{}
}

// set replaces the pending result; nil clears it.
func (p *pendingInventory) set(result *pipeline.ResolveResult) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.result = result
}

func (p *pendingInventory) flush(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	result := p.result
	p.result = nil
	p.mu.Unlock()
	if result != nil {
		pipeline.RecordResolvedInventory(ctx, *result)
	}
}
