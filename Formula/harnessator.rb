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
  version "0.16.0-beta" # bump:version
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_darwin_arm64.tar.gz"
      sha256 "57afe6649d2fe52fecd693c937ae9953d0bbf5a0b40b039bad828d460043e3d3" # bump:sha:darwin_arm64
    end
    on_intel do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_darwin_amd64.tar.gz"
      sha256 "4fd74233cc2b762bc1523bbf5bd3aa23745ce62df1cdf2a935463862b5d598cf" # bump:sha:darwin_amd64
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_linux_arm64.tar.gz"
      sha256 "830530b22e9dd0b806b08a24e1ba0674f8e939af85c4e3a7e6859d63587c5134" # bump:sha:linux_arm64
    end
    on_intel do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_linux_amd64.tar.gz"
      sha256 "3785a71cb6b5509066eea0060137cdbdfaad521061e3cf90a99b23e6859444c3" # bump:sha:linux_amd64
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
