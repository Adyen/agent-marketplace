package session

import (
	"sync"
	"testing"
)

func TestOperationGateSerializesBeginFinish(t *testing.T) {
	gate := NewOperationGate()
	first, ok := gate.Begin(OperationSync)
	if !ok {
		t.Fatal("first begin was rejected")
	}
	if _, ok := gate.Begin(OperationUpdate); ok {
		t.Fatal("overlapping begin was accepted")
	}
	if !gate.Accepts(first) {
		t.Fatal("gate does not accept its own operation")
	}
	gate.Finish(first)
	if gate.Accepts(first) {
		t.Fatal("gate still accepts a finished operation")
	}
	if _, ok := gate.Begin(OperationMutation); !ok {
		t.Fatal("begin after finish was rejected")
	}
}

func TestOperationGateFinishIgnoresStaleID(t *testing.T) {
	gate := NewOperationGate()
	active, ok := gate.Begin(OperationSync)
	if !ok {
		t.Fatal("begin was rejected")
	}
	gate.Finish(active + 100)
	if !gate.Accepts(active) {
		t.Fatal("stale finish released the active operation")
	}
}

func TestOperationGateActiveReportsIdentity(t *testing.T) {
	gate := NewOperationGate()
	if _, _, ok := gate.Active(); ok {
		t.Fatal("idle gate reported an active operation")
	}
	id, ok := gate.Begin(OperationUpdate)
	if !ok {
		t.Fatal("begin was rejected")
	}
	gotID, gotKind, active := gate.Active()
	if !active || gotID != id || gotKind != OperationUpdate {
		t.Fatalf("Active = (%d, %q, %v), want (%d, %q, true)", gotID, gotKind, active, id, OperationUpdate)
	}
	gate.Finish(id)
	if _, _, ok := gate.Active(); ok {
		t.Fatal("finished gate still reports an active operation")
	}
}

func TestOperationGateConcurrentBegin(t *testing.T) {
	gate := NewOperationGate()
	const attempts = 16
	var wg sync.WaitGroup
	wins := make(chan OperationID, attempts)
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if id, ok := gate.Begin(OperationSync); ok {
				wins <- id
			}
		}()
	}
	wg.Wait()
	close(wins)
	count := 0
	for range wins {
		count++
	}
	if count != 1 {
		t.Fatalf("concurrent begins succeeded %d times, want 1", count)
	}
}
