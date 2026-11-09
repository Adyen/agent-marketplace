class AgentMarketplace < Formula
  desc "Sync shared AI agent resources from Git-based or local registries"
  homepage "https://github.com/Adyen/agent-marketplace"
  version "5.1.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.1.1/agent-marketplace-v5.1.1-darwin-arm64", using: :nounzip
      sha256 "581ef833ea6e8d21d61918e2a98f0ec3b16ca50cbbcb4e8638733964a438c861"
    end
    on_intel do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.1.1/agent-marketplace-v5.1.1-darwin-amd64", using: :nounzip
      sha256 "dc56ffae970f6124dc48ead4de00da8b9e04aa312729692f183fef2a46a02f40"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.1.1/agent-marketplace-v5.1.1-linux-arm64", using: :nounzip
      sha256 "d76368f84df5334b1b0c38242464286ddc27b7201c1b8687cd43d3d7bb498f2b"
    end
    on_intel do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.1.1/agent-marketplace-v5.1.1-linux-amd64", using: :nounzip
      sha256 "9a988efd9a2e4bfad88112b5a33899c194be71ca7af5c0b22ab91c448f74ce1c"
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
    bin.install "agent-marketplace-v5.1.1-#{os}-#{arch}" => "agent-marketplace"
  end

  test do
    assert_equal "v5.1.1", shell_output("#{bin}/agent-marketplace --version").strip
  end
end
