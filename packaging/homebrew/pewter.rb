# TEMPLATE. Do not copy into a tap until a signed release exists.
#
# Goes in a tap repository named homebrew-tap, as Formula/pewter.rb, so users run:
#   brew install <owner>/tap/pewter
#
# Release checklist (done by packaging/homebrew/update-formula.sh on a real release):
#   1. set `version`
#   2. replace every sha256 below with the value from the SIGNED SHA256SUMS file
#   3. the sha256 lines still say REPLACE_WITH_REAL_SHA256 until then, and the
#      formula will refuse to install (Homebrew rejects the bad hash). That is intended.
#
# Homebrew verifies the sha256 only; the Ed25519 signature on SHA256SUMS is what
# vouches for these hashes, so only copy them from a verified SHA256SUMS.
class Pewter < Formula
  desc "Orchestration for GPU compute, from the terminal (alias: pew)"
  homepage "https://omegacompute.com"
  license "Apache-2.0"
  version "0.0.0" # REPLACE at release

  base = "https://omegacompute.com/cli/pewter-v#{version}"

  on_macos do
    on_arm do
      url "#{base}-darwin-arm64.tar.gz"
      sha256 "REPLACE_WITH_REAL_SHA256_DARWIN_ARM64" # PLACEHOLDER
    end
    on_intel do
      url "#{base}-darwin-amd64.tar.gz"
      sha256 "REPLACE_WITH_REAL_SHA256_DARWIN_AMD64" # PLACEHOLDER
    end
  end

  on_linux do
    on_arm do
      url "#{base}-linux-arm64.tar.gz"
      sha256 "REPLACE_WITH_REAL_SHA256_LINUX_ARM64" # PLACEHOLDER
    end
    on_intel do
      url "#{base}-linux-amd64.tar.gz"
      sha256 "REPLACE_WITH_REAL_SHA256_LINUX_AMD64" # PLACEHOLDER
    end
  end

  def install
    bin.install "pewter"
    bin.install_symlink "pewter" => "pew"
    generate_completions_from_executable(bin/"pewter", "completion", shells: [:bash, :zsh])
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/pewter version")
    assert_equal shell_output("#{bin}/pewter version"), shell_output("#{bin}/pew version")
  end
end
