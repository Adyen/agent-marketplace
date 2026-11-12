package config

import (
	"errors"
	"testing"
)

func TestV4RejectsRemovedGUIConfig(t *testing.T) {
	_, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\ngui:\n  version_check: false\n"), "org.yaml", true)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.unknown-field" || validation.Path != "gui" {
		t.Fatalf("error = %v, want v4.unknown-field for gui", err)
	}
}
