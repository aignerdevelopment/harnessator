{
  description = "harnessator - package manager and policy layer for AI coding-harness configuration";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      # version and sha256 lines are auto-bumped by the release pipeline
      # (scripts/bump-packages.sh) from the release's checksums file.
      version = "0.16.0-beta"; # bump:version
      tag = "v${version}";

      # hex sha256 straight from harnessator_${tag}_checksums.txt
      # (nix fetchurl accepts hex; one truth with install.sh and brew)
      sha256For = {
        "x86_64-linux"     = "3785a71cb6b5509066eea0060137cdbdfaad521061e3cf90a99b23e6859444c3"; # bump:sha:linux_amd64
        "aarch64-linux"     = "830530b22e9dd0b806b08a24e1ba0674f8e939af85c4e3a7e6859d63587c5134"; # bump:sha:linux_arm64
        "x86_64-darwin"     = "4fd74233cc2b762bc1523bbf5bd3aa23745ce62df1cdf2a935463862b5d598cf"; # bump:sha:darwin_amd64
        "aarch64-darwin"     = "57afe6649d2fe52fecd693c937ae9953d0bbf5a0b40b039bad828d460043e3d3"; # bump:sha:darwin_arm64
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
