{
  description = "harnessator - package manager and policy layer for AI coding-harness configuration";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      # version and sha256 lines are auto-bumped by the release pipeline
      # (scripts/bump-packages.sh) from the release's checksums file.
      version = "0.16.1-beta"; # bump:version
      tag = "v${version}";

      # hex sha256 straight from harnessator_${tag}_checksums.txt
      # (nix fetchurl accepts hex; one truth with install.sh and brew)
      sha256For = {
        "x86_64-linux"      = "a94e7cd37fc6e8b2dd013e7aae51c432ad338df212b6a663a15740d8cc1ac20c"; # bump:sha:linux_amd64
        "aarch64-linux"      = "a4e50e4e85aab79bc140a74a0b47c5f25d8b776a9429b6c2c6d8d09953cdfbb3"; # bump:sha:linux_arm64
        "x86_64-darwin"      = "573e3b705fbd421b055b7196b96f6fe276d11289d2c1ef26b887a6a6e40a2060"; # bump:sha:darwin_amd64
        "aarch64-darwin"      = "26d947aeb857515e20c9767e30d44fb30014a640b0ee56a7009b101358eb9caa"; # bump:sha:darwin_arm64
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
