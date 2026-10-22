package pipeline

import (
	"log/slog"
	"net/url"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
)

// remoteProbe reports whether git can list a remote without prompting.
// Tests replace it to avoid network access.
var remoteProbe = func(gitURL string) error {
	_, err := cache.DefaultBranch(gitURL)
	return err
}

// WebURLHTTPSFallback returns the anonymous HTTPS clone URL for a GitHub or
// GitLab web URL, or "" when rawURL is not one. ParseGitWebURL rewrites web
// URLs to SSH, which needs an SSH key even for public repositories. The
// fallback is always HTTPS, also for an http:// web URL, so it never
// downgrades acquisition to unencrypted HTTP.
func WebURLHTTPSFallback(rawURL string) string {
	parsed := ParseGitWebURL(rawURL)
	if !parsed.Ok {
		return ""
	}
	// Take the host from the web URL itself: an SSH address cannot carry a
	// port, so splitting parsed.GitURL at ':' would misplace one.
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return ""
	}
	project, ok := strings.CutPrefix(parsed.GitURL, "git@"+u.Host+":")
	if !ok || project == "" {
		return ""
	}
	return "https://" + u.Host + "/" + project + ".git"
}

// PreferReachableTransport returns gitURL unless it is the SSH URL derived
// from the web URL rawURL, SSH cannot reach it, and anonymous HTTPS can. In
// that case it returns the HTTPS URL so public repositories install on
// machines without SSH keys. SSH stays preferred whenever it works. allowed
// vets the HTTPS URL against registry policy before any probe; a refused URL
// keeps gitURL.
func PreferReachableTransport(rawURL, gitURL string, allowed func(fallback string) error) string {
	fallback := WebURLHTTPSFallback(rawURL)
	if fallback == "" || gitURL != ParseGitWebURL(rawURL).GitURL {
		return gitURL
	}
	if err := allowed(fallback); err != nil {
		slog.Debug("HTTPS fallback refused by registry policy", "https_url", fallback, "error", err)
		return gitURL
	}
	sshErr := remoteProbe(gitURL)
	if sshErr == nil {
		return gitURL
	}
	if err := remoteProbe(fallback); err != nil {
		slog.Debug("HTTPS fallback could not reach registry either", "https_url", fallback, "error", err)
		return gitURL
	}
	slog.Info("SSH could not reach registry; using anonymous HTTPS", "ssh_url", gitURL, "https_url", fallback, "ssh_error", sshErr)
	return fallback
}

// RegistryURLPolicy returns a PreferReachableTransport policy check that
// validates entry, with its URL replaced, against allowed_registries patterns.
func RegistryURLPolicy(entry config.RegistryEntry, patterns []string) func(string) error {
	return func(gitURL string) error {
		entry.URL = gitURL
		return config.ValidateRegistryURLs([]config.RegistryEntry{entry}, patterns)
	}
}

// configuredWebURLForm picks which form of a web URL a removal targets: the
// SSH URL, or the HTTPS URL an earlier install fell back to. Only entries
// matching the requested path and ref count. The target scope's root file
// decides first, then every merged entry from that scope (including its
// included files), then the rest of the merged configuration. Within one of
// those levels the SSH form wins; without any match gitURL is kept.
func configuredWebURLForm(target, merged *config.Config, scope config.Source, rawURL, gitURL, registryPath, ref string) string {
	fallback := WebURLHTTPSFallback(rawURL)
	if fallback == "" {
		return gitURL
	}
	for _, cfg := range []*config.Config{target, scopedRegistries(merged, scope), merged} {
		if cfg == nil {
			continue
		}
		if webURLFormMatches(cfg, gitURL, registryPath, ref) {
			return gitURL
		}
		if webURLFormMatches(cfg, fallback, registryPath, ref) {
			return fallback
		}
	}
	return gitURL
}

// scopedRegistries returns the merged entries that originate from scope.
func scopedRegistries(cfg *config.Config, scope config.Source) *config.Config {
	if cfg == nil {
		return nil
	}
	scoped := &config.Config{}
	for _, entry := range cfg.Registries {
		if entry.Source == scope {
			scoped.Registries = append(scoped.Registries, entry)
		}
	}
	return scoped
}

func webURLFormMatches(cfg *config.Config, gitURL, registryPath, ref string) bool {
	for _, entry := range cfg.Registries {
		if adHocRegistryEntryMatches(entry, gitURL, registryPath, ref, "") {
			return true
		}
	}
	return false
}
