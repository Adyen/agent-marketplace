package main

import (
	"fmt"
	"strings"
)

const privateDownloadStrategy = `class AgentMarketplacePrivateDownloadStrategy < NoUnzipCurlDownloadStrategy
  def fetch(timeout: nil)
    token = ENV["HOMEBREW_AGENT_MARKETPLACE_TOKEN"]
    odie "HOMEBREW_AGENT_MARKETPLACE_TOKEN is required to download this private release asset" if token.nil? || token.empty?

    headers = meta[:headers] ||= []
    auth = "Authorization: Bearer #{token}"
    headers << "Accept: application/octet-stream" << auth
    begin
      super
    rescue StandardError => e
      raise CurlDownloadStrategyError.new(url, e.message.gsub(token, "<redacted>"))
    ensure
      headers.delete("Accept: application/octet-stream")
      headers.delete(auth)
    end
  end
end

`

func renderFormula(repo, tag, version, visibility string, assets map[string]assetInfo) string {
	var b strings.Builder
	if visibility != "public" {
		b.WriteString(privateDownloadStrategy)
	}
	fmt.Fprintf(&b, `class AgentMarketplace < Formula
  desc "Sync shared AI agent resources from Git-based or local registries"
  homepage "https://github.com/%s"
  version %q
  license "Apache-2.0"

`, repo, version)
	writeBlock := func(archKeyword, platform string) {
		name := assetName(tag, platform)
		var url, using string
		if visibility == "public" {
			url = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, name)
			using = ", using: :nounzip"
		} else {
			url = fmt.Sprintf("https://api.github.com/repos/%s/releases/assets/%d", repo, assets[name].id)
			using = ", using: AgentMarketplacePrivateDownloadStrategy"
		}
		fmt.Fprintf(&b, "    %s do\n      url %q%s\n      sha256 %q\n    end\n", archKeyword, url, using, assets[name].sha256)
	}
	b.WriteString("  on_macos do\n")
	writeBlock("on_arm", "darwin-arm64")
	writeBlock("on_intel", "darwin-amd64")
	b.WriteString("  end\n\n  on_linux do\n")
	writeBlock("on_arm", "linux-arm64")
	writeBlock("on_intel", "linux-amd64")
	fmt.Fprintf(&b, `  end

  depends_on "git"

  def install
    arch = case Hardware::CPU.arch
    when :arm64, :aarch64 then "arm64"
    when :x86_64 then "amd64"
    else
      odie "agent-marketplace supports only arm64 and x86_64 CPUs"
    end
    os = if OS.mac?
      "darwin"
    elsif OS.linux?
      "linux"
    else
      odie "agent-marketplace supports only macOS and Linux"
    end
    bin.install "agent-marketplace-%s-#{os}-#{arch}" => "agent-marketplace"
  end

  test do
    assert_equal %q, shell_output("#{bin}/agent-marketplace --version").strip
  end
end
`, tag, tag)
	return b.String()
}
