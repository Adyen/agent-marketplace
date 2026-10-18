class AgentMarketplacePrivateDownloadStrategy < NoUnzipCurlDownloadStrategy
  def basename
    Pathname.new(meta.fetch(:filename))
  end

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

class AgentMarketplace < Formula
  desc "Sync shared AI agent resources from Git-based or local registries"
  homepage "https://github.com/Adyen/agent-marketplace"
  version "5.0.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://api.github.com/repos/Adyen/agent-marketplace/releases/assets/654689261", using: AgentMarketplacePrivateDownloadStrategy, filename: "agent-marketplace-v5.0.0-darwin-arm64"
      sha256 "b593f3a35d597912f24b1aa95b13446ecd37df72603978aa85bfc040edaf8fcd"
    end
    on_intel do
      url "https://api.github.com/repos/Adyen/agent-marketplace/releases/assets/654689259", using: AgentMarketplacePrivateDownloadStrategy, filename: "agent-marketplace-v5.0.0-darwin-amd64"
      sha256 "540d12a7d923e3239504e80d98ea901a8eeb78e1b7dcd011bcb898d4a42e713c"
    end
  end

  on_linux do
    on_arm do
      url "https://api.github.com/repos/Adyen/agent-marketplace/releases/assets/654689258", using: AgentMarketplacePrivateDownloadStrategy, filename: "agent-marketplace-v5.0.0-linux-arm64"
      sha256 "b3c41045ed4847379c0c0a0bdd99bb47ecefbc5519a9c9bd03f05d00956fc3ae"
    end
    on_intel do
      url "https://api.github.com/repos/Adyen/agent-marketplace/releases/assets/654689260", using: AgentMarketplacePrivateDownloadStrategy, filename: "agent-marketplace-v5.0.0-linux-amd64"
      sha256 "eb54cd8f8cdc50fa4f97c6c801e3a12d915004399ba03e5a8b01bd639eb6e2f9"
    end
  end

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
    bin.install "agent-marketplace-v5.0.0-#{os}-#{arch}" => "agent-marketplace"
  end

  test do
    assert_equal "v5.0.0", shell_output("#{bin}/agent-marketplace --version").strip
  end
end
