# 003 — Piper TTS on Apple Silicon stays on CPU (no GPU/ANE)

**Status:** decided · **Date:** 2026-07-12 · **Scope:** `sopds-tts-rs` on macOS

## Decision

On Apple Silicon, `sopds-tts-rs` runs Piper/VITS on the **CPU execution provider** and scales with
**parallel workers** (`fb2-to-wav.sh WORKERS=N`, `SOPDS_TTS_THREADS` per daemon). We do **not** pursue
CoreML/ANE/GPU or Metal for these models. This is a deliberate dead-end, documented so we don't
re-litigate it.

## Why not the GPU/ANE

The blocker is **VITS's data-dependent output length**, not the input shape:

- The duration predictor emits per-phoneme durations *at runtime*; the length regulator then expands
  the encoder output to a frame count ONNX shape-inference can't know. Fixing the input `seq_len`
  (`make_dynamic_shape_fixed`) removes one dynamic axis and leaves the load-bearing one. The real
  blocker ops: `Range` (builds the frame sequence up to summed predicted duration), `Expand`/`Tile`,
  `ConstantOfShape`. `onnx-simplifier` can't fold these — they're not constants.
- **CoreML MLProgram cannot represent unbounded ranges at all** (a coremltools limitation, not just
  ORT); NeuralNetwork can, but ORT then partitions the dynamic nodes back to CPU. Those are exactly
  the two failures we hit (MLProgram "unbounded dimension" spam; NeuralNetwork segfault) → full CPU
  fallback anyway.
- **Even if it ran on ANE, it'd likely be a wash or slower.** Piper on CPU is already ~5–10× real-time
  (fp16 RTF ≈ 0.2, real-time on a Raspberry Pi 5). ANE has ~0.1 ms fixed dispatch overhead per op and
  is bad at small/irregular graphs; the closest public data point (an ONNX speech model on M2 Max) ran
  *slower* on CoreML than CPU. Note even Piper's official **CUDA** path often loses to CPU.
- **Getting VITS onto the ANE at all** requires a Kokoro-style rework: multiple fixed-geometry *bucket*
  models with padding, `nn.Linear → Conv1d(1)` surgery, and keeping alignment/upsampling on the host.
  Weeks of work for, at best, a latency wash. No public "Piper faster on a Mac GPU" benchmark exists
  after many attempts — the absence is itself the evidence.

## Alternatives (rejected for now)

- **MLX** — no Piper/VITS port and no converter (full hand-rewrite). MeloTTS (VITS lineage) exists in
  `mlx-audio` as a possible starting point; the mature MLX TTS is **F5-TTS** (diffusion, not VITS) —
  see [001](001-f5-tts-onnx-integration.md).
- **coremltools direct / candle-metal / burn-wgpu / tract** — same dynamic-op wall or no clean VITS
  import; `ort`+CPU is the realistic Rust menu.

## Consequence

- macOS build pins `CPUExecutionProvider` (with a `SOPDS_TTS_THREADS` cap for parallel daemons).
- Throughput on Apple Silicon = multithreaded CPU × workers. Measured on an M5 Pro (5 P + 10 E cores):
  **~2×** from 4 workers, flat beyond that (bounded by performance-core count). For very large books, a
  real GPU box (fedya, GTX 1070) is simply the faster machine despite older hardware.

## Sources

- ORT make-dynamic-shape-fixed / CoreML EP: https://onnxruntime.ai/docs/tutorials/mobile/helpers/make-dynamic-shape-fixed.html · https://onnxruntime.ai/docs/execution-providers/CoreML-ExecutionProvider.html
- CoreML unbounded-range limit: https://apple.github.io/coremltools/docs-guides/source/flexible-inputs.html
- VITS ONNX/CoreML dynamic-op issues: coqui-ai/TTS #2937, #2779 · onnxruntime #16738
- ANE overhead / CoreML-slower-than-CPU: https://github.com/k2-fsa/sherpa-onnx/issues/2910 · https://maderix.substack.com/p/inside-the-m4-apple-neural-engine-615
- Working VITS-on-ANE reference (the cost): https://github.com/mattmireles/kokoro-coreml
- Whisper fixed-window contrast: https://github.com/ggml-org/whisper.cpp/discussions/548
- Piper CPU baseline / GPU stance: https://github.com/OHF-Voice/piper1-gpl · https://github.com/rhasspy/piper/discussions/544
