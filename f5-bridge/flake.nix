{
  description = "f5-bridge stress runtime — RUAccent (onnx, CPU) + tooling for fb2-to-f5.sh";

  # Phase 2a of auto-F5: the SYNTH half is native Rust (sopds-tts-rs/flake.nix, CUDA/Pascal);
  # this flake packages the STRESS half so `fb2-to-f5.sh` runs on Fedya WITHOUT pip/venvs.
  # RUAccent 1.5.8.3 loads ONNX accent models via onnxruntime — no torch, no CUDA — so the
  # stress env is a plain CPU python. Everything but RUAccent itself is already in nixpkgs.

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; config.allowUnfree = true; }; # _7zz-rar (RAR codec) is unfree
        py = pkgs.python3;

        # koziev — a 189 MB python+data subpackage RUAccent normally downloads into its own
        # (read-only in Nix) package dir and imports as `from .koziev...`. It MUST live inside
        # the package, so we fetch it at build (FOD) and bundle it in postInstall. dictionary+nn
        # models are read as data, so those still download at runtime into $RUACCENT_HOME.
        ruaccent-koziev = pkgs.stdenvNoCC.mkDerivation {
          name = "ruaccent-koziev";
          nativeBuildInputs = [ (py.withPackages (ps: [ ps.huggingface-hub ])) pkgs.cacert ];
          buildCommand = ''
            export SSL_CERT_FILE="${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
            export HF_HUB_DISABLE_TELEMETRY=1 HF_HUB_DISABLE_PROGRESS_BARS=1
            export HOME="$TMPDIR" HF_HOME="$TMPDIR/hf"
            mkdir -p "$out"
            python -c "from huggingface_hub import snapshot_download; snapshot_download('ruaccent/accentuator', allow_patterns=['koziev/**'], local_dir='$out')"
            rm -rf "$out/.cache"   # drop non-deterministic HF metadata
          '';
          outputHashMode = "recursive";
          outputHashAlgo = "sha256";
          outputHash = "sha256-E8SfhQulH96O3MDyNKOQcbDg+4N5984SGuYXOrXqDNc=";
        };

        # RUAccent — pure-python; dictionary+nn onnx models fetch from HF at runtime into
        # $RUACCENT_HOME (writable), koziev bundled at build. Only pkg missing from nixpkgs.
        ruaccent = py.pkgs.buildPythonPackage rec {
          pname = "ruaccent";
          version = "1.5.8.3";
          # pyproject.toml declares a flit backend but the sdist ships a real setup.py;
          # the legacy setuptools path builds cleanly and avoids the flit_core dep.
          format = "setuptools";
          src = py.pkgs.fetchPypi {
            inherit pname version;
            hash = "sha256-E0NNiUl5F1csplvh+LTfvtP0YhZX/TvPochE868l1f4=";
          };
          dependencies = with py.pkgs; [
            huggingface-hub
            onnxruntime
            transformers
            sentencepiece
            numpy
            python-crfsuite
            razdel
          ];
          # Honor $RUACCENT_HOME for the writable model workdir (default is the read-only
          # package dir, which fails in the Nix store).
          # Default the model workdir to a WRITABLE path ($RUACCENT_HOME or ~/.cache/ruaccent);
          # upstream defaults to the package dir, which is read-only in the Nix store.
          postPatch = ''
            substituteInPlace ruaccent/ruaccent.py \
              --replace-fail \
                'self.workdir = str(pathlib.Path(__file__).resolve().parent)' \
                'self.workdir = os.environ.get("RUACCENT_HOME") or os.path.expanduser("~/.cache/ruaccent")'
          '';
          # Bundle koziev into the installed package so `from .koziev...` resolves and the
          # runtime koziev-download branch (os.path.exists(module_path/koziev)) is skipped.
          postInstall = ''
            cp -r ${ruaccent-koziev}/koziev "$out"/${py.sitePackages}/ruaccent/koziev
          '';
          doCheck = false;
          pythonImportsCheck = [ "ruaccent" ];
        };

        # The RUPY interpreter that fb2-to-f5.sh calls for the stress phase.
        ruaccent-python = py.withPackages (_: [ ruaccent ]);
      in
      {
        packages = {
          inherit ruaccent ruaccent-python ruaccent-koziev;
          default = ruaccent-python;
        };

        # `nix develop` on Fedya: RUPY (stress) + 7z (audiobook packaging) + ffmpeg (mp3).
        # The SYNTH binary comes from the sopds-tts-rs flake (F5BIN), kept separate on purpose.
        devShells.default = pkgs.mkShell {
          name = "f5-bridge";
          packages = [
            ruaccent-python
            pkgs._7zz-rar # 7zz (modern 7-Zip + RAR) — pack chapters into the .7z the scanner ingests
            pkgs.ffmpeg   # wav → mp3 join in fb2-to-f5.sh
            pkgs.libxml2  # xmllint — fb2-to-f5.sh counts parts via XPath (xp())
            pkgs.python3  # fb2_extract.py (chapter split); stdlib-only, any python3
            pkgs.bash
          ];
          shellHook = ''
            export RUPY="${ruaccent-python}/bin/python"
            echo "f5-bridge stress shell"
            echo "  RUPY:   $RUPY"
            echo "  ruaccent: $("$RUPY" -c 'import ruaccent; print(ruaccent.__version__)' 2>/dev/null)"
            echo "  7zz:    $(command -v 7zz)   ffmpeg: $(command -v ffmpeg)"
            echo "Synth binary (F5BIN) comes from ../sopds-tts-rs (its own CUDA flake)."
          '';
        };
      });
}
