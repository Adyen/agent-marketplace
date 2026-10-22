package pipeline

import (
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestResolvePluginDeliveryPerTarget(t *testing.T) {
	tests := []struct {
		name        string
		request     string
		format      string
		target      target.Target
		policy      pluginpolicy.NativeDeliveryPolicy
		policyErr   error
		want        string
		wantCode    string
		wantRefresh string
	}{
		{
			name:    "auto on unverified target decomposes",
			request: "auto", format: "claude-plugin", target: target.TargetCursor,
			policy: pluginpolicy.NativeDeliveryPolicy{Status: pluginpolicy.DeliveryUnverified},
			want:   "decompose",
		},
		{
			name:    "auto on verified matching format is native",
			request: "auto", format: "claude-plugin", target: target.TargetClaude,
			policy: pluginpolicy.NativeDeliveryPolicy{Status: pluginpolicy.DeliveryVerified, Format: "claude-plugin", Refresh: pluginpolicy.NativeRefreshRestartRequired},
			want:   "native", wantRefresh: pluginpolicy.NativeRefreshRestartRequired,
		},
		{
			name:    "auto on verified other format decomposes",
			request: "auto", format: "claude-plugin", target: target.TargetClaude,
			policy: pluginpolicy.NativeDeliveryPolicy{Status: pluginpolicy.DeliveryVerified, Format: "codex-plugin"},
			want:   "decompose",
		},
		{
			name:    "explicit native on unverified target is refused",
			request: "native", format: "claude-plugin", target: target.TargetCursor,
			policy:   pluginpolicy.NativeDeliveryPolicy{Status: pluginpolicy.DeliveryUnverified},
			wantCode: diag.CodePluginDeliveryNativeUnverified,
		},
		{
			name:    "explicit native on verified target without layout is refused",
			request: "native", format: "claude-plugin", target: target.TargetDevin,
			policy:   pluginpolicy.NativeDeliveryPolicy{Status: pluginpolicy.DeliveryVerified, Format: "claude-plugin"},
			wantCode: diag.CodePluginRegistrationUnsupported,
		},
		{
			name:    "decompose does not depend on native catalog lookup",
			request: "decompose", format: "claude-plugin", target: target.TargetClaude,
			policyErr: errors.New("catalog unavailable"), want: "decompose",
		},
		{
			name:    "auto blocks when native policy lookup fails",
			request: "auto", format: "claude-plugin", target: target.TargetClaude,
			policyErr: errors.New("catalog unavailable"),
			wantCode:  diag.CodePluginDeliveryNativeUnverified,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := resolvePluginDeliveryPolicy(test.request, test.format, test.target, test.policy, test.policyErr)
			if got.Delivery != test.want {
				t.Fatalf("delivery = %q, want %q", got.Delivery, test.want)
			}
			if got.Refresh != test.wantRefresh {
				t.Errorf("refresh = %q, want %q", got.Refresh, test.wantRefresh)
			}
			if got.DiagnosticCode() != test.wantCode {
				t.Errorf("diagnostic code = %q, want %q", got.DiagnosticCode(), test.wantCode)
			}
		})
	}
}
