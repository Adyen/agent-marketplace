package lockfile

import (
	"net/url"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/gitidentity"
)

func CanonicalGitConfigURL(raw string) (string, error) {
	if filepath.IsAbs(raw) {
		raw = (&url.URL{Scheme: "file", Path: filepath.ToSlash(raw)}).String()
	}
	return CanonicalGitURL(raw)
}

func CanonicalGitURL(raw string) (string, error) { return CanonicalGitURLV1(raw) }

func CanonicalGitURLV1(raw string) (string, error) {
	canonical, err := gitidentity.CanonicalV1(raw)
	if err != nil {
		return "", coded(CodeInvalidGitURL, "%s", err)
	}
	return canonical, nil
}
