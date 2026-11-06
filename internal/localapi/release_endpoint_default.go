//go:build !e2e

package localapi

func releaseEndpoint() string {
	return githubReleaseURL
}
