{
  description = "Herdlord: monitor and access agents across multiple Herdr sessions";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        version = pkgs.lib.removeSuffix "\n" (builtins.readFile ./VERSION);
        commit = self.rev or self.dirtyRev or "dirty";
        modified = self.lastModifiedDate or null;
        date =
          if modified == null then "unknown"
          else "${builtins.substring 0 4 modified}-${builtins.substring 4 2 modified}-${builtins.substring 6 2 modified}T${builtins.substring 8 2 modified}:${builtins.substring 10 2 modified}:${builtins.substring 12 2 modified}Z";
        herdlord = pkgs.buildGoModule {
          pname = "herdlord";
          inherit version;
          src = self;
          subPackages = [ "cmd/herdlord" ];
          vendorHash = "sha256-FgjisVNuWlK1G2w9tY6nX5rbC+/oZI1lyEEixZDmnrI=";
          ldflags = [
            "-X github.com/mjrusso/herdlord/internal/buildinfo.Version=${version}"
            "-X github.com/mjrusso/herdlord/internal/buildinfo.Commit=${commit}"
            "-X github.com/mjrusso/herdlord/internal/buildinfo.Date=${date}"
          ];
        };
        ciPackages = with pkgs; [
          go
          git
          just
          goreleaser
          golangci-lint
          actionlint
        ];
      in {
        packages.herdlord = herdlord;
        packages.default = herdlord;
        apps.herdlord = flake-utils.lib.mkApp { drv = herdlord; };
        apps.default = self.apps.${system}.herdlord;
        devShells.ci = pkgs.mkShell { packages = ciPackages; };
        devShells.default = pkgs.mkShell {
          packages = ciPackages ++ [ pkgs.gopls pkgs.gotools ]
            ++ pkgs.lib.optionals pkgs.stdenv.hostPlatform.isLinux [ pkgs.inotify-tools pkgs.libnotify ]
            ++ pkgs.lib.optionals pkgs.stdenv.hostPlatform.isDarwin [ pkgs.terminal-notifier ];
        };
        checks.herdlord = herdlord;
      });
}
