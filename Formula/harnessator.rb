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
  version "0.16.1-beta" # bump:version
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_darwin_arm64.tar.gz"
      sha256 "26d947aeb857515e20c9767e30d44fb30014a640b0ee56a7009b101358eb9caa" # bump:sha:darwin_arm64
    end
    on_intel do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_darwin_amd64.tar.gz"
      sha256 "573e3b705fbd421b055b7196b96f6fe276d11289d2c1ef26b887a6a6e40a2060" # bump:sha:darwin_amd64
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_linux_arm64.tar.gz"
      sha256 "a4e50e4e85aab79bc140a74a0b47c5f25d8b776a9429b6c2c6d8d09953cdfbb3" # bump:sha:linux_arm64
    end
    on_intel do
      url "https://github.com/aignerdevelopment/harnessator/releases/download/v#{version}/harnessator_v#{version}_linux_amd64.tar.gz"
      sha256 "a94e7cd37fc6e8b2dd013e7aae51c432ad338df212b6a663a15740d8cc1ac20c" # bump:sha:linux_amd64
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
