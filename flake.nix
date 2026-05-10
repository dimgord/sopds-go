{
  description = "sopds-go — Self-hosted OPDS catalog server (Go + Rust subprojects)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";

    # NOTE: sopds-tts-rs/flake.nix is intentionally NOT composed as a
    # `path:./sopds-tts-rs` input here (Rev 67 had it; Rev 68 dropped
    # it). Reason: when this root flake is consumed via
    # `nix run github:dimgord/sopds-go`, Nix tries to re-lock the
    # `path:` input against the remote checkout location and fails with
    # `cannot write modified lock file of flake (use --no-write-lock-file
    # to ignore)`. That breaks the entire `nix run` UX for end users.
    #
    # The Rust TTS subproject remains a self-contained flake — users
    # who want the CUDA + Pascal sm_61 dev shell run
    # `cd sopds-tts-rs && nix develop ./`  from a local clone.
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
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

          # Filled in via `nix build .#sopds` (Rev 67) — Nix re-computes
          # this hash from go.sum + go.mod every build, so it must be
          # bumped whenever module deps change. CI doesn't cover this
          # automatically; bump it locally and commit.
          vendorHash = "sha256-/Bws/W3fmXls7i+4GN26S4kJt/+cpf9sKIqLaWhIImA=";

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

        # Go dev shell. `nix develop` from repo root puts you here.
        # For the Rust-CUDA workflow, the sopds-tts-rs/ subproject has
        # its own self-contained flake — `cd sopds-tts-rs && nix develop ./`
        # from a local clone. (See PROGRESS Rev 68 for why it's not
        # composed in here as an input.)
        devShells.default = pkgs.mkShell {
          name = "sopds-go";
          packages = with pkgs; [
            go_1_25
            gopls
            go-tools # staticcheck
            golangci-lint
            delve # debugger
            go-task # Taskfile.yml runner
            postgresql_18 # client tools (psql, pg_dump) for migrations / backups
          ];
          shellHook = ''
            echo "sopds-go dev shell on ${system}"
            echo "  go:           $(go version | awk '{print $3}')"
            echo "  golangci:     $(golangci-lint version 2>/dev/null | head -1)"
            echo "  task:         $(task --version 2>/dev/null | head -1)"
            echo
            echo "Quick refs:"
            echo "  task build                              build all binaries"
            echo "  task test                               go test ./..."
            echo "  cd sopds-tts-rs && nix develop ./       Rust + CUDA shell (Linux only)"
          '';
        };
      }
    );
}
