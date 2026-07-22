{
  description = "sopds-tts-rs - Rust TTS with CUDA GPU support";

  inputs = {
    # Latest nixpkgs for Rust toolchain, espeak-ng, etc.
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    # Pinned nixpkgs with cuDNN 9.8.0 (last version supporting Pascal/sm_61)
    nixpkgs-cuda.url = "github:NixOS/nixpkgs/e6f23dc08d3624daab7094b701aa3954923c6bbb";
    rust-overlay.url = "github:oxalica/rust-overlay";
    rust-overlay.inputs.nixpkgs.follows = "nixpkgs";
    # Fresh nixpkgs JUST for the RUAccent stress python — this flake's `nixpkgs` is pinned older for
    # CUDA/Rust and its python3.13 lacks onnxruntime. Matches f5-bridge/flake.nix (python3.14 + onnx).
    nixpkgs-stress.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs, nixpkgs-cuda, rust-overlay, nixpkgs-stress }:
    let
      system = "x86_64-linux";

      # Main pkgs: latest toolchain
      pkgs = import nixpkgs {
        inherit system;
        config.allowUnfree = true;
        overlays = [ rust-overlay.overlays.default ];
      };

      # CUDA pkgs: pinned to cuDNN 9.8 (Pascal support)
      cudaPkgs = import nixpkgs-cuda {
        inherit system;
        config = {
          allowUnfree = true;
          cudaSupport = true;
          cudaCapabilities = [ "6.1" ];  # GTX 1070 (Pascal)
          cudaForwardCompat = false;
        };
      };

      cudaPackages = cudaPkgs.cudaPackages_12;

      onnxruntime = cudaPkgs.onnxruntime.override {
        cudaSupport = true;
        cudaPackages = cudaPackages;
      };

      rustToolchain = pkgs.rust-bin.stable.latest.default;

      # RUAccent stress runtime, INLINED so this flake needs no cross-flake path input (keeps the
      # lock stable + avoids the git-ignore/lock churn a `path:../f5-bridge` input caused). This is a
      # byte-for-byte copy of the derivation in f5-bridge/flake.nix — keep the two in sync until the
      # RUAccent→Rust native port lands (which deletes both).
      stressPkgs = import nixpkgs-stress { inherit system; config.allowUnfree = true; };
      py = stressPkgs.python3;
      ruaccent-koziev = pkgs.stdenvNoCC.mkDerivation {
        name = "ruaccent-koziev";
        nativeBuildInputs = [ (py.withPackages (ps: [ ps.huggingface-hub ])) pkgs.cacert ];
        buildCommand = ''
          export SSL_CERT_FILE="${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt"
          export HF_HUB_DISABLE_TELEMETRY=1 HF_HUB_DISABLE_PROGRESS_BARS=1
          export HOME="$TMPDIR" HF_HOME="$TMPDIR/hf"
          mkdir -p "$out"
          python -c "from huggingface_hub import snapshot_download; snapshot_download('ruaccent/accentuator', allow_patterns=['koziev/**'], local_dir='$out')"
          rm -rf "$out/.cache"
        '';
        outputHashMode = "recursive";
        outputHashAlgo = "sha256";
        outputHash = "sha256-E8SfhQulH96O3MDyNKOQcbDg+4N5984SGuYXOrXqDNc=";
      };
      ruaccent = py.pkgs.buildPythonPackage rec {
        pname = "ruaccent";
        version = "1.5.8.3";
        format = "setuptools";
        src = py.pkgs.fetchPypi {
          inherit pname version;
          hash = "sha256-E0NNiUl5F1csplvh+LTfvtP0YhZX/TvPochE868l1f4=";
        };
        dependencies = with py.pkgs; [
          huggingface-hub onnxruntime transformers sentencepiece numpy python-crfsuite razdel
        ];
        postPatch = ''
          substituteInPlace ruaccent/ruaccent.py \
            --replace-fail \
              'self.workdir = str(pathlib.Path(__file__).resolve().parent)' \
              'self.workdir = os.environ.get("RUACCENT_HOME") or os.path.expanduser("~/.cache/ruaccent")'
          for m in accent_model omograph_model yo_homograph_model stress_usage_model; do
            substituteInPlace ruaccent/$m.py \
              --replace-quiet 'self.session.run(None, inputs)' \
                              'self.session.run(None, {n.name: (inputs[n.name] if n.name in inputs else inputs["input_ids"] * 0) for n in self.session.get_inputs()})'
          done
        '';
        postInstall = ''
          cp -r ${ruaccent-koziev}/koziev "$out"/${py.sitePackages}/ruaccent/koziev
        '';
        doCheck = false;
        pythonImportsCheck = [ "ruaccent" ];
      };
      ruaccentPython = py.withPackages (_: [ ruaccent ]);

      # Host NVIDIA driver libs (not provided by Nix on non-NixOS); shared by both shells.
      nvidiaHook = ''
        NVIDIA_DRIVER_DIR="$(mktemp -d)"
        for lib in /usr/lib64/libcuda.so* /usr/lib64/libnvidia-*.so*; do
          [ -e "$lib" ] && ln -sf "$lib" "$NVIDIA_DRIVER_DIR/"
        done
        export LD_LIBRARY_PATH="''${LD_LIBRARY_PATH:+$LD_LIBRARY_PATH:}$NVIDIA_DRIVER_DIR"
      '';
    in
    {
      devShells.${system} = {
        default = pkgs.mkShell {
        name = "sopds-tts-rs";

        nativeBuildInputs = [
          rustToolchain
          pkgs.pkg-config
        ];

        buildInputs = [
          onnxruntime
          cudaPackages.cudatoolkit
          cudaPackages.cudnn
          pkgs.espeak-ng
          pkgs.openssl
        ];

        env = {
          ORT_PREFER_DYNAMIC_LINK = "1";
          ORT_LIB_LOCATION = "${onnxruntime}/lib";
        };

        shellHook = nvidiaHook + ''
          echo "sopds-tts-rs dev shell (cuDNN 9.8 for Pascal/sm_61)"
          echo "ONNX Runtime: ${onnxruntime}"
          echo "espeak-ng: $(which espeak-ng)"
        '';
      };

        # Combined shell to RUN the auto-F5 worker end-to-end: the CUDA runtime for F5BIN plus the full
        # fb2-to-f5.sh toolchain, with RUPY/F5PY preset. `nix develop ./sopds-tts-rs#worker` — no more
        # nix-shell layering or manual exports.
        worker = pkgs.mkShell {
        name = "f5-worker";
        buildInputs = [
          onnxruntime
          cudaPackages.cudatoolkit
          cudaPackages.cudnn
          pkgs.espeak-ng
          pkgs.openssl
        ];
        packages = [
          ruaccentPython # RUPY — RUAccent stress (onnx, CPU)
          pkgs.python3   # F5PY + fb2_extract.py (stdlib only)
          pkgs.gawk      # chunk splitting in fb2-to-f5.sh
          pkgs.libxml2   # xmllint — XPath part/title extraction
          pkgs.ffmpeg    # wav → mp3 join
          pkgs._7zz-rar  # 7zz — audiobook .7z packaging (unfree; allowed above)
          pkgs.bash
        ];
        env = {
          ORT_PREFER_DYNAMIC_LINK = "1";
          ORT_LIB_LOCATION = "${onnxruntime}/lib";
        };
        shellHook = nvidiaHook + ''
          export RUPY="${ruaccentPython}/bin/python"
          export F5PY="${pkgs.python3}/bin/python3"
          echo "f5-worker shell (CUDA + RUAccent + tools)"
          echo "  RUPY=$RUPY"
          echo "  F5PY=$F5PY"
          echo "  7zz/ffmpeg/gawk/xmllint ready"
          echo "Run: cd <sopds-go> && ./sopds tts-worker -c config.yaml"
        '';
        };
      };
    };
}
