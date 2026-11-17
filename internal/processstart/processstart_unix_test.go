//go:build unix

package processstart

import (
	"context"
	"testing"
)

func TestCommandLeavesSysProcAttrUnsetAndDetachedSetsid(t *testing.T) {
	ctx := context.Background()

	command := Command(ctx, "true")
	if command.SysProcAttr != nil {
		t.Fatalf("Command SysProcAttr = %#v, want nil", command.SysProcAttr)
	}

	detached := Detached(ctx, "true")
	if detached.SysProcAttr == nil || !detached.SysProcAttr.Setsid {
		t.Fatalf("Detached SysProcAttr = %#v, want Setsid", detached.SysProcAttr)
	}
}
