{
  description = "sopds-tts-rs - Rust TTS with CUDA GPU support";

  inputs = {
    # Latest nixpkgs for Rust toolchain, espeak-ng, etc.
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    # Pinned nixpkgs with cuDNN 9.8.0 (last version supporting Pascal/sm_61)
    nixpkgs-cuda.url = "github:NixOS/nixpkgs/e6f23dc08d3624daab7094b701aa3954923c6bbb";
    rust-overlay.url = "github:oxalica/rust-overlay";
    rust-overlay.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { self, nixpkgs, nixpkgs-cuda, rust-overlay }:
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
    in
    {
      devShells.${system}.default = pkgs.mkShell {
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

        shellHook = ''
          # Host NVIDIA driver libs (not provided by Nix on non-NixOS)
          NVIDIA_DRIVER_DIR="$(mktemp -d)"
          for lib in /usr/lib64/libcuda.so* /usr/lib64/libnvidia-*.so*; do
            [ -e "$lib" ] && ln -sf "$lib" "$NVIDIA_DRIVER_DIR/"
          done
          export LD_LIBRARY_PATH="''${LD_LIBRARY_PATH:+$LD_LIBRARY_PATH:}$NVIDIA_DRIVER_DIR"
          echo "sopds-tts-rs dev shell (cuDNN 9.8 for Pascal/sm_61)"
          echo "ONNX Runtime: ${onnxruntime}"
          echo "espeak-ng: $(which espeak-ng)"
        '';
      };
    };
}
