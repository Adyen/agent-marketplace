package pipeline

import (
	"fmt"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/pluginpolicy"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

type PluginDeliveryResolution struct {
	Delivery   string
	Refresh    string
	Diagnostic *diag.Diagnostic
}

func ResolvePluginDelivery(request, format, sourceKind string, targetHarness hcc.Target) PluginDeliveryResolution {
	if sourceKind == "git" {
		return PluginDeliveryResolution{Diagnostic: &diag.Diagnostic{
			Code: diag.CodePluginDeliveryPending, Severity: diag.SeverityWarning,
			Target:  string(targetHarness),
			Message: "external plugin package acquisition is not available for delivery",
		}}
	}
	if request == "decompose" {
		return PluginDeliveryResolution{Delivery: "decompose"}
	}
	policy, err := pluginpolicy.LookupNativeDelivery(target.Target(targetHarness))
	return resolvePluginDeliveryPolicy(request, format, target.Target(targetHarness), policy, err)
}

func resolvePluginDeliveryPolicy(request, format string, targetHarness target.Target, policy pluginpolicy.NativeDeliveryPolicy, policyErr error) PluginDeliveryResolution {
	if request == "" {
		request = "auto"
	}
	if request == "decompose" {
		return PluginDeliveryResolution{Delivery: "decompose"}
	}
	if policyErr != nil {
		return pluginDeliveryBlocked(diag.CodePluginDeliveryNativeUnverified, targetHarness,
			"native plugin delivery could not be verified for this target")
	}
	selectedNative := request == "native"
	if request == "auto" {
		selectedNative = policy.Status == pluginpolicy.DeliveryVerified && policy.Format == format
	}
	if !selectedNative {
		return PluginDeliveryResolution{Delivery: "decompose"}
	}
	if policy.Status != pluginpolicy.DeliveryVerified || policy.Format != format {
		return pluginDeliveryBlocked(diag.CodePluginDeliveryNativeUnverified, targetHarness,
			"native plugin delivery is not verified for this package format")
	}
	if _, ok := target.NativePluginRegistration(targetHarness); !ok {
		return pluginDeliveryBlocked(diag.CodePluginRegistrationUnsupported, targetHarness,
			"the target has no native plugin registration layout")
	}
	return PluginDeliveryResolution{Delivery: "native", Refresh: policy.Refresh}
}

func pluginDeliveryBlocked(code string, targetHarness target.Target, message string) PluginDeliveryResolution {
	return PluginDeliveryResolution{Diagnostic: &diag.Diagnostic{
		Code: code, Severity: diag.SeverityError, Target: string(targetHarness), Message: message,
	}}
}

func (r PluginDeliveryResolution) DiagnosticCode() string {
	if r.Diagnostic == nil {
		return ""
	}
	return r.Diagnostic.Code
}

func (r PluginDeliveryResolution) String() string {
	if r.Diagnostic != nil {
		return fmt.Sprintf("blocked (%s)", r.Diagnostic.Code)
	}
	if r.Delivery == "decompose" {
		return "decomposed"
	}
	return r.Delivery
}
