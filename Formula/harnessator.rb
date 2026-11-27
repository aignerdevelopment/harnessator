# typed: false
# frozen_string_literal: true

# Harnessator homebrew tap formula. The GitHub repository doubles as the
# tap: `brew install aignerdevelopment/harnessator/harnessator`.
# version and sha256 lines are auto-bumped by the release pipeline
# (scripts/bump-packages.sh) from the release's checksums file - do not
# hand-edit them; the `# bump:` markers are the machine anchors.

class Harnessator < Formula
  desc "Package manager and policy layer for AI coding-harness configuration"
  homepage "https://github.com/aignerdevelopment/harnessator"
  version "1.0.0" # bump:version
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_darwin_arm64.tar.gz"
      sha256 "7a695c401583def001fb04dca4f1c48433f81535ab0c541e133e509a0b2ae0b5" # bump:sha:darwin_arm64
    end
    on_intel do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_darwin_amd64.tar.gz"
      sha256 "cf6b74b137c7e3475e736f4c806fa4589a9f14242c80dc503f65872e28333057" # bump:sha:darwin_amd64
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_linux_arm64.tar.gz"
      sha256 "eafcc7b05b90fdab151b07613db180fdefaaebc6c05276351119e47e562e1b18" # bump:sha:linux_arm64
    end
    on_intel do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_linux_amd64.tar.gz"
      sha256 "4259dfec67961fcc32fa355013e69d62de1917f1632ca6ff99401ac4807d1046" # bump:sha:linux_amd64
    end
  end

  # native windows: irm https://raw.githubusercontent.com/aignerdevelopment/harnessator/main/install.ps1 | iex

  def install
    binary = Dir.children(buildpath)
                .grep(/\Aharnessator_/)
                .map { |name| buildpath/name }
                .find { |path| path.file? && path.executable? }
    odie "harnessator binary not found in the release archive" if binary.nil?
    bin.install binary => "harnessator"
    license_file = Dir.children(buildpath).find { |name| name.casecmp("LICENSE").zero? }
    prefix.install buildpath/license_file if license_file
  end

  test do
    # the release binary embeds its version; the install is proven by
    # the CLI answering with the tool's own contract surface
    assert_match version.to_s, shell_output("#{bin}/harnessator version")
  end
end
