class AgentMarketplace < Formula
  desc "Sync shared AI agent resources from Git-based or local registries"
  homepage "https://github.com/Adyen/agent-marketplace"
  version "5.0.3"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.0.3/agent-marketplace-v5.0.3-darwin-arm64", using: :nounzip
      sha256 "8f814eea9319847662abad767b6341c544a2efeddb0b5322b33e310ba5a9d6e5"
    end
    on_intel do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.0.3/agent-marketplace-v5.0.3-darwin-amd64", using: :nounzip
      sha256 "ddf5483399ee4a7d3be49cc4e82ca040a77c94070ff5214623d6e83581a12ae9"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.0.3/agent-marketplace-v5.0.3-linux-arm64", using: :nounzip
      sha256 "46bab47032c45c3403d78b78bd6814e017a6723f06f2b9dbe861687090bcf1db"
    end
    on_intel do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.0.3/agent-marketplace-v5.0.3-linux-amd64", using: :nounzip
      sha256 "d9cab6c8e9b0207b8e6ce8d60200729c60feca698940a534746c3ab3e6e4a287"
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
    bin.install "agent-marketplace-v5.0.3-#{os}-#{arch}" => "agent-marketplace"
  end

  test do
    assert_equal "v5.0.3", shell_output("#{bin}/agent-marketplace --version").strip
  end
end
