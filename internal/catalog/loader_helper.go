package catalog

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// registryCacheAttrs builds safe span attributes describing the registry cache
// work about to be performed. It deliberately omits credentials, query
// strings, and fragments; only URL scheme, host, and path are recorded.
func registryCacheAttrs(cfg *config.Config, jobs []*fetchJob, mode LoadMode) map[string]string {
	attrs := map[string]string{
		"mode":           mode.String(),
		"registry.count": strconv.Itoa(len(cfg.Registries)),
		"job.count":      strconv.Itoa(len(jobs)),
	}

	var schemes, hosts, paths, aliases []string
	for _, e := range cfg.Registries {
		if e.IsLocal() {
			continue
		}
		aliases = append(aliases, registryAlias(e.URL))
		u, err := url.Parse(e.URL)
		if err != nil {
			paths = append(paths, e.URL)
			continue
		}
		if u.Scheme != "" {
			schemes = append(schemes, u.Scheme)
		}
		if u.Host != "" {
			hosts = append(hosts, u.Host)
		}
		if u.Path != "" {
			paths = append(paths, u.Path)
		}
	}
	if len(schemes) > 0 {
		attrs["registry.url.scheme"] = strings.Join(schemes, ",")
	}
	if len(hosts) > 0 {
		attrs["registry.url.host"] = strings.Join(hosts, ",")
	}
	if len(paths) > 0 {
		attrs["registry.url.path"] = strings.Join(paths, ",")
	}
	if len(aliases) > 0 {
		attrs["registry.alias"] = strings.Join(aliases, ",")
	}

	localCount := 0
	for _, e := range cfg.Registries {
		if e.IsLocal() {
			localCount++
		}
	}
	attrs["local.count"] = strconv.Itoa(localCount)
	return attrs
}

func registryCacheResultAttrs(jobs []*fetchJob) map[string]string {
	errs := 0
	for _, j := range jobs {
		if j.err != nil {
			errs++
		}
	}
	attrs := map[string]string{"fetch.error_count": strconv.Itoa(errs)}
	if errs > 0 {
		attrs["error"] = "true"
	}
	return attrs
}

func catalogIngestAttrs(result LoadResult) map[string]string {
	entryCount := 0
	malformedCount := 0
	for _, cat := range result.Catalogs {
		entryCount += len(cat.Entries)
		malformedCount += len(cat.Malformed)
	}
	attrs := map[string]string{
		"catalog.count":   strconv.Itoa(len(result.Catalogs)),
		"entry.count":     strconv.Itoa(entryCount),
		"malformed.count": strconv.Itoa(malformedCount),
		"error.count":     strconv.Itoa(len(result.Errors)),
		"warning.count":   strconv.Itoa(len(result.Collections)),
	}
	if len(result.Errors) > 0 {
		attrs["error"] = "true"
		if code := firstCodedError(result.Errors); code != "" {
			attrs["error.code"] = code
		}
	}
	return attrs
}

func firstCodedError(errs []error) string {
	for _, err := range errs {
		if coded, ok := diag.AsCodedError(err); ok && coded.Code != "" {
			return coded.Code
		}
	}
	return ""
}

func registryAlias(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Path != "" {
		base := u.Path
		base = strings.TrimSuffix(base, ".git")
		base = strings.Trim(base, "/")
		if i := strings.LastIndex(base, "/"); i >= 0 {
			return base[i+1:]
		}
		return base
	}
	rawURL = strings.TrimSuffix(rawURL, ".git")
	if i := strings.LastIndex(rawURL, "/"); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}
