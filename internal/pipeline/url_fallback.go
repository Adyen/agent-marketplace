package pipeline

import (
	"log/slog"
	"net/url"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
)

// remoteProbe returns the branch a remote advertises as HEAD, failing when
// git cannot list the remote without prompting or a credential helper. Tests
// replace it to avoid network access.
var remoteProbe = cache.ProbeDefaultBranch

// WebURLHTTPSFallback returns the HTTPS clone URL for a GitHub or GitLab web
// URL, or "" when rawURL is not one. ParseGitWebURL rewrites web URLs to SSH,
// which needs an SSH key even for public repositories. The fallback is always
// HTTPS, also for an http:// web URL, so it never downgrades acquisition to
// unencrypted HTTP.
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

// TransportChoice is the URL PreferReachableTransport selected.
type TransportChoice struct {
	// URL is the Git URL to use.
	URL string
	// DefaultBranch is the branch URL advertised as HEAD when a probe reached
	// it, so callers need not list the remote again; "" when nothing was
	// probed or the probe did not reach URL.
	DefaultBranch string
	// FellBackFrom is the unreachable SSH URL when URL is the HTTPS fallback,
	// and "" otherwise.
	FellBackFrom string
}

// Notice describes a fallback for the user, such as "SSH could not reach A;
// recorded B instead" for verb "recorded", or returns "" when there was none.
func (c TransportChoice) Notice(verb string) string {
	if c.FellBackFrom == "" {
		return ""
	}
	return "SSH could not reach " + c.FellBackFrom + "; " + verb + " " + c.URL + " instead"
}

// PreferReachableTransport keeps gitURL unless it is the SSH URL derived from
// the web URL rawURL, SSH cannot reach it, and HTTPS can. In that case it
// selects the HTTPS URL so public repositories install on machines without
// SSH keys. SSH stays preferred whenever it works. allowed vets the HTTPS URL
// against registry policy before any probe; a refused URL keeps gitURL
// without probing.
func PreferReachableTransport(rawURL, gitURL string, allowed func(fallback string) error) TransportChoice {
	fallback := WebURLHTTPSFallback(rawURL)
	if fallback == "" || gitURL != ParseGitWebURL(rawURL).GitURL {
		return TransportChoice{URL: gitURL}
	}
	if err := allowed(fallback); err != nil {
		slog.Debug("HTTPS fallback refused by registry policy", "https_url", fallback, "error", err)
		return TransportChoice{URL: gitURL}
	}
	branch, sshErr := remoteProbe(gitURL)
	if sshErr == nil {
		return TransportChoice{URL: gitURL, DefaultBranch: branch}
	}
	branch, err := remoteProbe(fallback)
	if err != nil {
		slog.Debug("HTTPS fallback could not reach registry either", "https_url", fallback, "error", err)
		return TransportChoice{URL: gitURL}
	}
	slog.Info("SSH could not reach registry; using HTTPS", "ssh_url", gitURL, "https_url", fallback, "ssh_error", sshErr)
	return TransportChoice{URL: fallback, DefaultBranch: branch, FellBackFrom: gitURL}
}

// SelectAdHocTransport chooses the Git URL install --from records for rawURL.
// A form that the merged configuration already records for the repository
// (and that allowed accepts) wins without probing, so install never adds a
// second source for the same repository; otherwise PreferReachableTransport
// decides.
func SelectAdHocTransport(merged *config.Config, scope config.Source, rawURL, gitURL, registryPath, ref string, allowed func(string) error) TransportChoice {
	if configured, ok := configuredWebURLMatch(nil, merged, scope, rawURL, gitURL, registryPath, ref); ok && allowed(configured) == nil {
		return TransportChoice{URL: configured}
	}
	return PreferReachableTransport(rawURL, gitURL, allowed)
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
// SSH URL, or the HTTPS URL an earlier install fell back to. Without a
// configured match it keeps gitURL; see configuredWebURLMatch.
func configuredWebURLForm(target, merged *config.Config, scope config.Source, rawURL, gitURL, registryPath, ref string) string {
	if form, ok := configuredWebURLMatch(target, merged, scope, rawURL, gitURL, registryPath, ref); ok {
		return form
	}
	return gitURL
}

// configuredWebURLMatch reports which form of the web URL rawURL is already
// configured: gitURL (its SSH form) or its HTTPS fallback. Only entries
// matching registryPath and ref count. The target scope's root file decides
// first, then every merged entry from that scope (including its included
// files), then the rest of the merged configuration. Within one of those
// levels the SSH form wins.
func configuredWebURLMatch(target, merged *config.Config, scope config.Source, rawURL, gitURL, registryPath, ref string) (string, bool) {
	fallback := WebURLHTTPSFallback(rawURL)
	if fallback == "" {
		return "", false
	}
	for _, cfg := range []*config.Config{target, scopedRegistries(merged, scope), merged} {
		if cfg == nil {
			continue
		}
		if webURLFormMatches(cfg, gitURL, registryPath, ref) {
			return gitURL, true
		}
		if webURLFormMatches(cfg, fallback, registryPath, ref) {
			return fallback, true
		}
	}
	return "", false
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
