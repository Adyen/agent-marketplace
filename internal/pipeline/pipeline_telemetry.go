package pipeline

import (
	"strconv"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func installOptionAttrs(opts InstallOptions) map[string]string {
	attrs := map[string]string{
		"scope":         scopeLabel(opts.Scope),
		"all":           strconv.FormatBool(opts.InstallAll),
		"tag":           strconv.FormatBool(opts.Tag != ""),
		"from":          strconv.FormatBool(opts.FromURL != ""),
		"resource.kind": kindLabel(opts.Kind, opts.HasKind),
	}
	if opts.Registry != "" {
		attrs["registry.filter"] = "true"
	}
	return attrs
}

func installResultAttrs(result *InstallResult, err error) map[string]string {
	attrs := map[string]string{}
	if result != nil {
		attrs["installed.count"] = strconv.Itoa(len(result.Installed))
		attrs["collection"] = strconv.FormatBool(result.IsCollection)
	}
	if err != nil {
		attrs["error"] = "true"
		if code := firstErrorCode([]error{err}); code != "" {
			attrs["error.code"] = code
		}
	}
	return attrs
}

func scopeLabel(s Scope) string {
	switch s {
	case ScopeUser:
		return "user"
	case ScopeRepo:
		return "repo"
	case ScopeLocal:
		return "local"
	}
	return "unknown"
}

func kindLabel(kind ir.ResourceKind, hasKind bool) string {
	if !hasKind {
		return "auto"
	}
	return kind.String()
}

func updateOptionAttrs(opts UpdateOptions) map[string]string {
	return map[string]string{
		"bump": strconv.FormatBool(opts.Bump),
	}
}

func updateResultAttrs(result *UpdateResult, err error) map[string]string {
	attrs := map[string]string{}
	if result != nil {
		attrs["updated_registries"] = strconv.Itoa(result.UpdatedRegistries)
		attrs["bump.count"] = strconv.Itoa(len(result.Bumps))
	}
	if err != nil {
		attrs["error"] = "true"
		if code := firstErrorCode([]error{err}); code != "" {
			attrs["error.code"] = code
		}
	}
	return attrs
}

func updateRefsAttrs(repoCfg *config.Config) map[string]string {
	count := 0
	if repoCfg != nil {
		for _, e := range repoCfg.Registries {
			if !e.IsLocal() {
				count++
			}
		}
	}
	return map[string]string{"registry.count": strconv.Itoa(count)}
}

func updateRefsResultAttrs(bumps []RefBump, err error) map[string]string {
	attrs := map[string]string{"bump.count": strconv.Itoa(len(bumps))}
	if err != nil {
		attrs["error"] = "true"
		if code := firstErrorCode([]error{err}); code != "" {
			attrs["error.code"] = code
		}
	}
	return attrs
}

func removeOptionAttrs(opts RemoveOptions) map[string]string {
	return map[string]string{
		"scope":         scopeLabel(opts.Scope),
		"from":          strconv.FormatBool(opts.FromURL != ""),
		"resource.kind": kindLabel(opts.Kind, opts.HasKind),
	}
}

func removeResultAttrs(result *RemoveResult, err error) map[string]string {
	attrs := map[string]string{}
	if result != nil {
		attrs["removed.count"] = strconv.Itoa(1)
		attrs["removed_registry"] = strconv.FormatBool(result.RemovedRegistry)
		attrs["excluded_registry"] = strconv.FormatBool(result.ExcludedRegistry)
		attrs["collection"] = strconv.FormatBool(result.IsCollection)
	}
	if err != nil {
		attrs["error"] = "true"
		if code := firstErrorCode([]error{err}); code != "" {
			attrs["error.code"] = code
		}
	}
	return attrs
}

func resolveResultAttrs(result ResolveResult, d time.Duration) map[string]string {
	attrs := map[string]string{
		"resolved.count":      strconv.Itoa(len(result.Resolved)),
		"config_error.count":  strconv.Itoa(len(result.ConfigErrors)),
		"catalog_error.count": strconv.Itoa(len(result.CatalogErrors)),
		"resolve_error.count": strconv.Itoa(len(result.ResolveErrors)),
		"error.count":         strconv.Itoa(len(result.Errors)),
		"duration_ms":         strconv.FormatInt(d.Milliseconds(), 10),
	}
	if result.MergedCfg != nil {
		attrs["registry.count"] = strconv.Itoa(len(result.MergedCfg.Registries))
	}
	if code := firstErrorCode(result.Errors); code != "" {
		attrs["error.code"] = code
	}
	return attrs
}

func configLoadAttrs(ec config.EffectiveConfig) map[string]string {
	attrs := map[string]string{
		"source.org":   strconv.FormatBool(ec.Organization.Config != nil),
		"source.user":  strconv.FormatBool(ec.User.Config != nil),
		"source.repo":  strconv.FormatBool(ec.Repo.Config != nil),
		"source.local": strconv.FormatBool(ec.Local != nil),
		"error.count":  strconv.Itoa(len(ec.Errors)),
	}
	if code := firstErrorCode(ec.Errors); code != "" {
		attrs["error.code"] = code
	}
	return attrs
}

func selectionAttrs(result ResolveResult) map[string]string {
	kindSet := make(map[string]struct{})
	for _, r := range result.Resolved {
		kindSet[r.Kind.String()] = struct{}{}
	}
	kinds := make([]string, 0, len(kindSet))
	for k := range kindSet {
		kinds = append(kinds, k)
	}
	attrs := map[string]string{
		"resolved.count": strconv.Itoa(len(result.Resolved)),
		"error.count":    strconv.Itoa(len(result.Errors)),
		"warning.count":  strconv.Itoa(len(result.Warnings)),
	}
	if len(kinds) > 0 {
		attrs["resource.kinds"] = strings.Join(kinds, ",")
	}
	if code := firstErrorCode(result.Errors); code != "" {
		attrs["error.code"] = code
	}
	return attrs
}

func syncResultAttrs(result SyncResult, d time.Duration) map[string]string {
	written := 0
	removed := 0
	targets := 0
	if result.Report != nil {
		targets = len(result.Report.PerRoot)
		for _, t := range result.Report.PerRoot {
			written += len(t.Written)
			removed += len(t.Removed)
		}
	}
	attrs := map[string]string{
		"resolved.count":          strconv.Itoa(len(result.Resolved)),
		"written.count":           strconv.Itoa(written),
		"removed.count":           strconv.Itoa(removed),
		"target.count":            strconv.Itoa(targets),
		"warning.count":           strconv.Itoa(len(result.Warnings)),
		"error.count":             strconv.Itoa(len(result.Errors)),
		"materialize_error.count": strconv.Itoa(len(result.MaterializeErrors)),
		"duration_ms":             strconv.FormatInt(d.Milliseconds(), 10),
	}
	if code := firstErrorCode(result.Errors); code != "" {
		attrs["error.code"] = code
	}
	return attrs
}

func materializationResultAttrs(report *materialize.Report, err error) map[string]string {
	attrs := map[string]string{}
	errCount := 0
	warnCount := 0
	if report != nil {
		for _, d := range report.Diags {
			switch d.Severity {
			case diag.SeverityError:
				errCount++
			case diag.SeverityWarning:
				warnCount++
			}
		}
	}
	if err != nil {
		errCount++
	}
	attrs["diagnostic.error.count"] = strconv.Itoa(errCount)
	attrs["diagnostic.warning.count"] = strconv.Itoa(warnCount)
	if err != nil {
		attrs["error"] = "true"
		if code := firstErrorCode([]error{err}); code != "" {
			attrs["error.code"] = code
		}
	}
	return attrs
}

func firstErrorCode(errs []error) string {
	for _, err := range errs {
		if coded, ok := diag.AsCodedError(err); ok && coded.Code != "" {
			return coded.Code
		}
	}
	return ""
}
