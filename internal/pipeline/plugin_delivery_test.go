package pipeline

import (
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// spec: plugin-delivery/auto-delivers-claude-plugins-natively-at-user-scope-only
func TestResolvePluginDeliveryUsesScopeSpecificPolicy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scope Scope
		want  string
	}{
		{name: "user", scope: ScopeUser, want: "native"},
		{name: "repo", scope: ScopeRepo, want: "decompose"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolvePluginDelivery("auto", "claude-plugin", "claude", tc.scope)
			if got.Delivery != tc.want || got.Diagnostic != nil {
				t.Fatalf("Claude delivery at %s = %#v, want %q without a diagnostic", tc.scope, got, tc.want)
			}
		})
	}
}

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
			name:    "auto keeps agent-plugin packages decomposed",
			request: "auto", format: "agent-plugin", target: target.TargetClaude,
			policy: pluginpolicy.NativeDeliveryPolicy{Status: pluginpolicy.DeliveryVerified, Format: "claude-plugin"},
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
