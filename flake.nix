{
  description = "harnessator - package manager and policy layer for AI coding-harness configuration";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      # version and sha256 lines are auto-bumped by the release pipeline
      # (scripts/bump-packages.sh) from the release's checksums file.
      version = "1.0.0"; # bump:version
      tag = "v${version}";

      # hex sha256 straight from harnessator_${tag}_checksums.txt
      # (nix fetchurl accepts hex; one truth with install.sh and brew)
      sha256For = {
        "x86_64-linux"       = "4259dfec67961fcc32fa355013e69d62de1917f1632ca6ff99401ac4807d1046"; # bump:sha:linux_amd64
        "aarch64-linux"       = "eafcc7b05b90fdab151b07613db180fdefaaebc6c05276351119e47e562e1b18"; # bump:sha:linux_arm64
        "x86_64-darwin"       = "cf6b74b137c7e3475e736f4c806fa4589a9f14242c80dc503f65872e28333057"; # bump:sha:darwin_amd64
        "aarch64-darwin"       = "7a695c401583def001fb04dca4f1c48433f81535ab0c541e133e509a0b2ae0b5"; # bump:sha:darwin_arm64
      };

      assetFor = {
        "x86_64-linux" = "linux_amd64";
        "aarch64-linux" = "linux_arm64";
        "x86_64-darwin" = "darwin_amd64";
        "aarch64-darwin" = "darwin_arm64";
      };

      mkPackage = system:
        let pkgs = import nixpkgs { inherit system; };
        in pkgs.stdenvNoCC.mkDerivation {
          pname = "harnessator";
          inherit version;

          src = pkgs.fetchurl {
            url = "https://github.com/aignerdevelopment/harnessator/releases/download/${tag}/harnessator_${tag}_${assetFor.${system}}.tar.gz";
            sha256 = sha256For.${system};
          };

          dontConfigure = true;
          dontBuild = true;

          installPhase = ''
            runHook preInstall
            mkdir -p "$out/bin"
            bin="$(find . -maxdepth 1 -type f -name 'harnessator_*' | head -n1)"
            install -m755 "$bin" "$out/bin/harnessator"
            if [ -f LICENSE ]; then install -Dm644 LICENSE "$out/share/licenses/harnessator/LICENSE"; fi
            runHook postInstall
          '';
        };
    in {
      packages = builtins.listToAttrs (map
        (system: {
          name = system;
          value = {
            default = mkPackage system;
            harnessator = mkPackage system;
          };
        })
        (builtins.attrNames sha256For));
    };
}
