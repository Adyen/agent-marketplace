package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const maxMetadataBytes = 1 << 20
const devinManifest = "https://static.devin.ai/cli/current/manifest.json"

type release struct {
	Target       string `json:"target"`
	CLI          string `json:"cli"`
	Version      string `json:"version"`
	NPMPackage   string `json:"npm_package"`
	BundleURL    string `json:"bundle_url"`
	BundleSHA256 string `json:"bundle_sha256"`
}

var stablePattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\+[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*)?$`)

func stableVersion(version string) bool { return stablePattern.MatchString(version) }

func resolveVersion(name string, fetch func(string) ([]byte, error)) (release, error) {
	capability, declared := capabilities[name]
	if !target.IsValid(name) || !declared || capability.cli == "" {
		return release{}, fmt.Errorf("target has no declared native release source")
	}
	result := release{Target: name, CLI: capability.cli, NPMPackage: capability.npm}
	endpoint := devinManifest
	if capability.npm != "" {
		endpoint = "https://registry.npmjs.org/" + url.PathEscape(capability.npm) + "/latest"
	}
	body, err := fetch(endpoint)
	if err != nil {
		return release{}, fmt.Errorf("public release metadata fetch failed")
	}
	if len(body) > maxMetadataBytes {
		return release{}, fmt.Errorf("release metadata exceeds size limit")
	}
	if capability.npm != "" {
		var document struct{ Name, Version string }
		if err := json.Unmarshal(body, &document); err != nil || document.Name != capability.npm {
			return release{}, fmt.Errorf("invalid npm latest metadata or package identity")
		}
		result.Version = document.Version
	} else {
		var document struct {
			Version   string
			Platforms map[string]struct{ URL, SHA256 string }
		}
		if err := json.Unmarshal(body, &document); err != nil {
			return release{}, fmt.Errorf("invalid Devin release manifest")
		}
		bundle := document.Platforms["x86_64-unknown-linux"]
		if !trustedBundle(bundle.URL) || !regexp.MustCompile(`^[a-fA-F0-9]{64}$`).MatchString(bundle.SHA256) {
			return release{}, fmt.Errorf("Devin manifest lacks a trusted Linux bundle URL and SHA256")
		}
		result.Version, result.BundleURL, result.BundleSHA256 = document.Version, bundle.URL, strings.ToLower(bundle.SHA256)
	}
	if !stableVersion(result.Version) {
		return release{}, fmt.Errorf("latest release is not a stable semantic version; no prerelease or older-version fallback is permitted")
	}
	return result, nil
}

func cleanHTTPS(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	return u, err == nil && u.Scheme == "https" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.Opaque == ""
}

func trustedBundle(raw string) bool {
	u, ok := cleanHTTPS(raw)
	return ok && u.Host == "static.devin.ai" && u.RawPath == "" && strings.HasPrefix(u.Path, "/cli/") && path.Clean(u.Path) == u.Path && regexp.MustCompile(`^/cli/[A-Za-z0-9._/-]+$`).MatchString(u.Path)
}

func trustedMetadata(raw string) bool {
	u, ok := cleanHTTPS(raw)
	if !ok {
		return false
	}
	if raw == devinManifest {
		return true
	}
	if u.Host != "registry.npmjs.org" {
		return false
	}
	for _, capability := range capabilities {
		if capability.npm != "" && u.Path == "/"+capability.npm+"/latest" {
			return true
		}
	}
	return false
}

func metadataClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) == 0 || len(via) > 4 || next.URL.Host != via[0].URL.Host || !trustedMetadata(next.URL.String()) {
				return fmt.Errorf("untrusted metadata redirect refused")
			}
			return nil
		},
	}
}

func fetchMetadata(client *http.Client, endpoint string) ([]byte, error) {
	if !trustedMetadata(endpoint) {
		return nil, fmt.Errorf("untrusted metadata endpoint refused")
	}
	response, err := client.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("metadata request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil || len(body) > maxMetadataBytes {
		return nil, fmt.Errorf("metadata body unreadable or over size limit")
	}
	return body, nil
}
