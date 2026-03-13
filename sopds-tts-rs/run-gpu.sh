#!/bin/bash
# Wrapper to run sopds-tts-rs with CUDA GPU support.
# Sets LD_LIBRARY_PATH to find:
#   - onnxruntime 1.24.3 with CUDA provider (from pip onnxruntime-gpu)
#   - cuDNN 9.5 (from pip nvidia-cudnn-cu12)
#   - CUDA runtime libs

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PYTHON_SITE="/home/dimgord/.local/lib/python3.14/site-packages"

export LD_LIBRARY_PATH="/home/dimgord/ort-cuda-lib:${PYTHON_SITE}/onnxruntime/capi:${PYTHON_SITE}/nvidia/cudnn/lib:${PYTHON_SITE}/nvidia/cublas/lib:/usr/local/cuda/targets/x86_64-linux/lib"

exec "${SCRIPT_DIR}/target/release/sopds-tts-rs" "$@"
