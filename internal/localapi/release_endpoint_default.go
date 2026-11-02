//go:build !e2e

package localapi

func releaseEndpoint() string {
	return githubReleaseURL
}

func allowedReleasePageURL(raw string) bool {
	return isGitHubReleaseURL(raw)
}

func allowedReleaseAssetURL(raw, tag, name string) bool {
	return isGitHubReleaseAssetURL(raw, tag, name)
}
