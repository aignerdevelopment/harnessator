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
  version "0.15.5-beta" # bump:version
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_darwin_arm64.tar.gz"
      sha256 "bcf7397c78177624b26d94b73727c62e90920b4a9ca750b5b71b4b036eedbc52" # bump:sha:darwin_arm64
    end
    on_intel do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_darwin_amd64.tar.gz"
      sha256 "8dee5b4071b50b9253085455e4696148f5d19392d465271cf9e16710688aa0f2" # bump:sha:darwin_amd64
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_linux_arm64.tar.gz"
      sha256 "12845931b1671ac00f768b8db4429751212a0da027f145d93f703de9d4a1a071" # bump:sha:linux_arm64
    end
    on_intel do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_linux_amd64.tar.gz"
      sha256 "141689d3154aa15899f3399ddd6b022ca825713619a760ac235839b9d9072b1b" # bump:sha:linux_amd64
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
