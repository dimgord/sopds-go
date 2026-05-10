{
  description = "sopds-go — Self-hosted OPDS catalog server (Go + Rust subprojects)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";

    # The Rust TTS subproject has its own flake (CUDA + Pascal sm_61
    # bring-up was non-trivial — see sopds-tts-rs/flake.nix and PROGRESS
    # Rev 52). Composed here so a single root `nix develop` from the
    # repo root has access to everything; users who only need the Rust
    # path can still `cd sopds-tts-rs && nix develop ./` directly.
    sopds-tts-rs = {
      url = "path:./sopds-tts-rs";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      sopds-tts-rs,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfree = true;
        };

        # Common ldflags for buildGoModule — same as GoReleaser uses for
        # tagged releases (see .goreleaser.yaml). For `nix build` outside
        # a tag, version is the short git rev (or "dev" if dirty).
        version =
          if (self ? rev) then
            (builtins.substring 0 7 self.rev)
          else if (self ? dirtyRev) then
            "dev-dirty"
          else
            "dev";

        commonGoArgs = {
          inherit version;
          src = ./.;

          # First build will fail with the expected hash — copy that
          # value here, then re-run. Or use `nix-prefetch` derivative
          # tooling if vendoring drift is a concern.
          vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];

          # Don't run package tests during `nix build` — the test data
          # paths and PostgreSQL fixtures live outside this derivation.
          # CI runs tests separately (.github/workflows/ci.yml).
          doCheck = false;

          meta = with pkgs.lib; {
            license = licenses.agpl3Plus;
            homepage = "https://github.com/dimgord/sopds-go";
            maintainers = [ ];
          };
        };
      in
      {
        # Three packages, one per CLI binary. `default` aliases sopds.
        packages = {
          sopds = pkgs.buildGoModule (
            commonGoArgs
            // {
              pname = "sopds";
              subPackages = [ "cmd/sopds" ];
              meta = commonGoArgs.meta // {
                description = "Self-hosted OPDS catalog server";
                mainProgram = "sopds";
              };
            }
          );

          sopds-tts = pkgs.buildGoModule (
            commonGoArgs
            // {
              pname = "sopds-tts";
              subPackages = [ "cmd/sopds-tts" ];
              meta = commonGoArgs.meta // {
                description = "TTS subprocess for sopds-go (Go path; see sopds-tts-rs/ for CUDA-accelerated alternative)";
                mainProgram = "sopds-tts";
              };
            }
          );

          zipdupes = pkgs.buildGoModule (
            commonGoArgs
            // {
              pname = "zipdupes";
              subPackages = [ "cmd/zipdupes" ];
              meta = commonGoArgs.meta // {
                description = "FB2-archive duplicate finder (see zipdupes-rs/ for the Rust port)";
                mainProgram = "zipdupes";
              };
            }
          );

          default = self.packages.${system}.sopds;
        };

        # `nix run github:dimgord/sopds-go -- start` invokes sopds.
        # `nix run github:dimgord/sopds-go#sopds-tts -- <args>` for the TTS binary.
        apps =
          let
            mkApp = name: {
              type = "app";
              program = "${self.packages.${system}.${name}}/bin/${name}";
            };
          in
          {
            sopds = mkApp "sopds";
            sopds-tts = mkApp "sopds-tts";
            zipdupes = mkApp "zipdupes";
            default = mkApp "sopds";
          };

        # Default Go dev shell. `nix develop` from repo root puts you
        # here. For the Rust-CUDA workflow, see `tts-rs` shell below.
        devShells = {
          default = pkgs.mkShell {
            name = "sopds-go";
            packages = with pkgs; [
              go_1_25
              gopls
              go-tools # staticcheck
              golangci-lint
              delve # debugger
              go-task # Taskfile.yml runner
              postgresql_16 # client tools (psql, pg_dump) for migrations / backups
            ];
            shellHook = ''
              echo "sopds-go dev shell on ${system}"
              echo "  go:           $(go version | awk '{print $3}')"
              echo "  golangci:     $(golangci-lint version 2>/dev/null | head -1)"
              echo "  task:         $(task --version 2>/dev/null | head -1)"
              echo
              echo "Quick refs:"
              echo "  task build         build all binaries"
              echo "  task test          go test ./..."
              echo "  nix develop .#tts-rs    Rust + CUDA shell (Linux only)"
            '';
          };

          # Re-export sopds-tts-rs's CUDA-aware shell. Linux-only because
          # the upstream flake hardcodes x86_64-linux + cudaSupport.
        }
        // pkgs.lib.optionalAttrs (system == "x86_64-linux") {
          tts-rs = sopds-tts-rs.devShells.${system}.default;
        };
      }
    );
}
