package resolver

import (
	"errors"
	"fmt"
	"testing"
)

// spec: resource-selection/collection-expansion-stays-within-the-work-limit
func TestResolveRefusesCollectionExpansionAboveWorkLimit(t *testing.T) {
	for _, tc := range []struct {
		depth   int
		refused bool
	}{{8, false}, {12, true}} {
		t.Run(fmt.Sprintf("depth %d", tc.depth), func(t *testing.T) {
			cfg, catalogs := collectionDiamond(tc.depth)
			resolved, errs := Resolve(cfg, catalogs, nil)
			refused := false
			for _, err := range errs {
				if errors.Is(err, ErrCollectionExpansionLimit) {
					refused = true
				} else if !tc.refused {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if refused != tc.refused {
				t.Fatalf("depth %d: refused=%t, want %t (errs=%v)", tc.depth, refused, tc.refused, errs)
			}
			if !tc.refused && findResolved(resolved, "leaf-skill") == nil {
				t.Fatalf("depth %d: shared leaf skill was not expanded", tc.depth)
			}
		})
	}
}
