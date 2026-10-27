class AgentMarketplace < Formula
  desc "Sync shared AI agent resources from Git-based or local registries"
  homepage "https://github.com/Adyen/agent-marketplace"
  version "5.1.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.1.0/agent-marketplace-v5.1.0-darwin-arm64", using: :nounzip
      sha256 "3f94cdde2ca81566351a2339d4c627458d1024ea2bfd3f06a9e0c0ae62af450f"
    end
    on_intel do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.1.0/agent-marketplace-v5.1.0-darwin-amd64", using: :nounzip
      sha256 "9fa0b4469d9551ee45f6d10a8bfcfe5d0a61d5e6ce3e91cf4addfbbf4979303f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.1.0/agent-marketplace-v5.1.0-linux-arm64", using: :nounzip
      sha256 "3bf15512d9f32d92ad825f5f3d8dacb4a65ac242102d8318efadb9d72c56aa12"
    end
    on_intel do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.1.0/agent-marketplace-v5.1.0-linux-amd64", using: :nounzip
      sha256 "5dccf00308fbed72883f8b1342923b79930c4d4d9a4922b9dd4ec74007dbb664"
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
    bin.install "agent-marketplace-v5.1.0-#{os}-#{arch}" => "agent-marketplace"
  end

  test do
    assert_equal "v5.1.0", shell_output("#{bin}/agent-marketplace --version").strip
  end
end
