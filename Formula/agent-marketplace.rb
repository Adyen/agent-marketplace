class AgentMarketplace < Formula
  desc "Sync shared AI agent resources from Git-based or local registries"
  homepage "https://github.com/Adyen/agent-marketplace"
  version "5.0.2"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.0.2/agent-marketplace-v5.0.2-darwin-arm64", using: :nounzip
      sha256 "2e899d2ae111d9d16fef464f1355a7c4762b215c588a81d6ad96dad9f3ea8a09"
    end
    on_intel do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.0.2/agent-marketplace-v5.0.2-darwin-amd64", using: :nounzip
      sha256 "f4ae19aeab0e6cec2925168344085025b8341ca2e583e267a3555125c5565953"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.0.2/agent-marketplace-v5.0.2-linux-arm64", using: :nounzip
      sha256 "42c1055dec3777d0eb6ea6e35c63523a9ba1aea3636f0be493c32b340cfe3f1a"
    end
    on_intel do
      url "https://github.com/Adyen/agent-marketplace/releases/download/v5.0.2/agent-marketplace-v5.0.2-linux-amd64", using: :nounzip
      sha256 "9f213234c3139f98cf82d2c845bd44b8970f34c03fc57ac0bffbb0d91322ffe9"
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
    bin.install "agent-marketplace-v5.0.2-#{os}-#{arch}" => "agent-marketplace"
  end

  test do
    assert_equal "v5.0.2", shell_output("#{bin}/agent-marketplace --version").strip
  end
end
