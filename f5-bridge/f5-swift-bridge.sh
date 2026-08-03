#!/usr/bin/env bash
# Drop-in $F5BIN for f5-bridge/fb2-to-f5.sh — native Swift/MLX F5 synth on the Mac GPU
# (zero Python at runtime; MLX only runs on Apple Silicon, so this path is Mac-only — on
# Linux/Fedya keep the CUDA-ONNX `sopds-tts-rs` binary as F5BIN instead).
#
# Same contract as the Rust daemon: arg1 = voice dir; reads {"text","output"} NDJSON on
# stdin, writes each wav to its output path, exits on EOF. The pipeline waits on the wav
# files (it does not parse our stdout), so the daemon's @@READY/@@OK chatter is harmless.
#
# Voice dir must hold: model.safetensors, vocab.txt, ref.wav (24 kHz mono), ref.txt (the
# transcript in «+»-before-vowel stress notation, e.g. "Мен+е зв+ати Дмитр+о.").
# Env: F5SWIFT_BIN, VOCOS_DIR, F5SWIFT_STEPS (rk4 ODE steps, default 32), F5SWIFT_SEED (0).
#
# 32 steps, not 6: at 6 the ODE is nowhere near converged (the 6-step and 32-step results for one
# seed correlate only 0.08) and words come out mangled. It costs ~5x the time (~50s vs ~9s per 17s
# line on an M-series GPU) — lower it only for quick drafts.
set -euo pipefail
MODEL="${1:?usage: f5-swift-bridge.sh <voice-dir>}"
BIN="${F5SWIFT_BIN:-$HOME/src/f5-spike/uk-editor/f5bin/f5-tts-generate}"
export VOCOS_DIR="${VOCOS_DIR:-$HOME/src/f5-spike/vocos-mlx-swift}"
exec "$BIN" --serve \
  --model "$MODEL" \
  --ref-audio-path "$MODEL/ref.wav" \
  --ref-audio-text "$(cat "$MODEL/ref.txt")" \
  --steps "${F5SWIFT_STEPS:-32}" --seed "${F5SWIFT_SEED:-0}"
