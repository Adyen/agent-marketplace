package config

import (
	"fmt"
	"reflect"
	"testing"
)

func TestV4RequestReplayAllocationScaling(t *testing.T) {
	for _, mode := range []string{"rebuild-decisions", "rebuild-fallback", "effective-decisions"} {
		t.Run(mode, func(t *testing.T) {
			allocations := func(count int) float64 {
				group := V4ResourceGroup{Include: make([]V4ResourceRequest, count)}
				for i := range group.Include {
					request := &group.Include[i]
					*request = V4ResourceRequest{Locator: fmt.Sprintf("skill/item-%04d", i)}
					if mode != "rebuild-fallback" {
						group.Decisions = append(group.Decisions, V4SelectionDecision{
							Action: V4SelectionExplicit, Locator: request.Locator, Request: request,
						})
					}
				}
				cfg := &V4Config{Groups: map[string]V4ResourceGroup{"g": group}}
				return testing.AllocsPerRun(10, func() {
					var got []V4ResourceRequest
					if mode == "effective-decisions" {
						got = effectiveV4Requests(group)
					} else {
						cfg.rebuildRequests()
						got = cfg.Requests
					}
					if !reflect.DeepEqual(got, group.Include) {
						t.Fatal("request replay changed retained values or lexical order")
					}
				})
			}
			const small, large = 64, 1024
			before, after := allocations(small), allocations(large)
			// Allow container growth, but reject an allocation for each retained request.
			if growth := after - before; growth >= float64(large-small)/4 {
				t.Fatalf("allocations grow with requests: %.0f at %d, %.0f at %d", before, small, after, large)
			}
		})
	}
}

func TestV4RequestReplayReturnsValueCopies(t *testing.T) {
	for _, mode := range []string{"rebuild-decisions", "rebuild-fallback", "effective-decisions"} {
		t.Run(mode, func(t *testing.T) {
			group := V4ResourceGroup{Include: []V4ResourceRequest{
				{Locator: "skill/z", Name: "z", Options: V4ResourceOptions{As: "last", HasAs: true}},
				{Locator: "skill/a", Name: "a", Required: true},
			}}
			if mode != "rebuild-fallback" {
				for i := range group.Include {
					group.Decisions = append(group.Decisions, V4SelectionDecision{
						Action: V4SelectionExplicit, Locator: group.Include[i].Locator, Request: &group.Include[i],
					})
				}
			}
			cfg := &V4Config{Groups: map[string]V4ResourceGroup{"g": group}}
			var got []V4ResourceRequest
			if mode == "effective-decisions" {
				got = effectiveV4Requests(group)
			} else {
				cfg.rebuildRequests()
				got = cfg.Requests
			}
			want := []V4ResourceRequest{
				{Locator: "skill/a", Name: "a", Required: true},
				{Locator: "skill/z", Name: "z", Options: V4ResourceOptions{As: "last", HasAs: true}},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("requests = %#v, want %#v", got, want)
			}
			group.Include[0].Name = "changed input"
			if !reflect.DeepEqual(got, want) {
				t.Fatal("changing an input struct changed the emitted request")
			}
			got[0].Required = false
			got[0].Options.As = "changed output"
			if !group.Include[1].Required || group.Include[1].Options.HasAs || group.Include[1].Options.As != "" {
				t.Fatal("changing an emitted struct changed the input request")
			}
		})
	}
}
