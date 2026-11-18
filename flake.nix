{
  description = "harnessator - package manager and policy layer for AI coding-harness configuration";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      # version and sha256 lines are auto-bumped by the release pipeline
      # (scripts/bump-packages.sh) from the release's checksums file.
      version = "0.15.5-beta"; # bump:version
      tag = "v${version}";

      # hex sha256 straight from harnessator_${tag}_checksums.txt
      # (nix fetchurl accepts hex; one truth with install.sh and brew)
      sha256For = {
        "x86_64-linux"    = "141689d3154aa15899f3399ddd6b022ca825713619a760ac235839b9d9072b1b"; # bump:sha:linux_amd64
        "aarch64-linux"    = "12845931b1671ac00f768b8db4429751212a0da027f145d93f703de9d4a1a071"; # bump:sha:linux_arm64
        "x86_64-darwin"    = "8dee5b4071b50b9253085455e4696148f5d19392d465271cf9e16710688aa0f2"; # bump:sha:darwin_amd64
        "aarch64-darwin"    = "bcf7397c78177624b26d94b73727c62e90920b4a9ca750b5b71b4b036eedbc52"; # bump:sha:darwin_arm64
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
