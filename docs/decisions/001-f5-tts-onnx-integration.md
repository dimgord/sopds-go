# 001 — F5-TTS (Russian) ONNX backend for sopds-tts-rs

**Status:** exploratory (branch `tts-quality`) · **Date:** 2026-07-12 · **Scope:** `sopds-tts-rs`

## Problem

Piper caps at **medium** quality for Russian/Ukrainian (no `-high` voices exist). We want a
genuinely higher-quality Russian voice for audiobooks while keeping the ONNX-Runtime-only Rust
runner (no PyTorch/libtorch, no Python at synth time).

From the survey ([research report](../tts-quality-research.md) / PROGRESS Rev 82 context),
**F5-TTS** is the *only* higher-tier model that is both Russian-capable **and** has a real,
full-ONNX inference pipeline. Kokoro (nicer than Piper) has **no ru/uk** at all; Silero (best free
Russian) ships **PyTorch only**; XTTS is PyTorch + a dead licensor.

## What F5-TTS is

A 336M-parameter **flow-matching DiT** text-to-speech model. Unlike Piper's single VITS graph, an
F5 inference is a **3-stage pipeline** + an iterative sampler:

1. **Preprocess** — tokenize target text (+ a *reference* audio clip and its transcript; F5 is a
   zero-shot voice-cloning model, so it always conditions on a reference).
2. **Transformer (DiT)** — run the flow-matching ODE for **N function evaluations** (NFE, e.g. 16–32
   steps), producing a mel spectrogram. This is the expensive part.
3. **Vocoder** — mel → 24 kHz waveform (Vocos).

- **Russian weights:** `Misha24-10/F5-TTS_RUSSIAN` (a ~5000 h finetune, uses **RUAccent** for stress).
- **ONNX export:** `DakeQQ/F5-TTS-ONNX` exports the pipeline to ONNX-Runtime with pre-built CPU/GPU
  variants; an Apple-Silicon MLX port also exists.
- **License:** weights **CC-BY-NC** (fine for Dmitry's personal, non-distributed use; blocks
  redistribution). Code MIT.
- **Speed:** slower-than-realtime on CPU (batch/overnight); GPU RTFs are published but not for a
  GTX 1070 — must measure.

## Why it does NOT drop into the current runner

`sopds-tts-rs` today assumes the Piper contract: `espeak-ng → phoneme IDs → [input, input_lengths,
scales] → 22.05 kHz PCM`. F5 breaks every part of that:

| Aspect | Piper (today) | F5-TTS |
|---|---|---|
| Front-end | espeak-ng IPA → `phoneme_id_map` | RUAccent stress + F5 tokenizer/vocab |
| Graphs | 1 ONNX session | 3 ONNX sessions (preprocess / DiT / vocoder) |
| Conditioning | none | **reference audio + its transcript** (voice cloning) |
| Inference | single forward pass | **NFE-step ODE loop** in the DiT |
| Sample rate | 22050 | 24000 |
| Extra deps | — | RUAccent (Python? — availability in Rust is an open question) |

## Proposed approach

**Do not touch the Piper path.** Add F5 as a *separate model type*, ideally behind the same CLI so
sopds-go can pick it via `models_dir`/voice config.

1. **Prototype outside the main binary first.** Stand up `DakeQQ/F5-TTS-ONNX` (its Python runner) to
   pin the exact ONNX I/O — tensor names, shapes, dtypes for all 3 graphs — and to hear the Russian
   finetune quality on our hardware before committing Rust code. Deliverable: a notes file with the
   verified tensor signatures + a sample WAV.
2. **Reference audio + transcript.** Pick one fixed narrator clip (a few seconds) + its exact
   transcript, ship it alongside the model. All chunks clone that voice → consistent narration.
3. **Rust integration (a new `src/f5.rs`), gated by model-type detection:**
   - Detect F5 vs Piper by a marker in the model dir (e.g. a `f5.json` / directory layout) rather
     than overloading the Piper `.onnx.json`.
   - Load the 3 ORT sessions.
   - Front-end: RUAccent stress. **Open question** — is there a Rust RUAccent, or do we call it as a
     subprocess (like espeak), or port the rules? This is the biggest unknown.
   - Implement the NFE sampler loop (flow-matching Euler steps) in Rust around the DiT session.
   - Vocoder session → 24 kHz PCM → resample to 22050 if we want a uniform pipeline (or keep 24 k).
4. **Keep the daemon protocol identical** (NDJSON in, WAV out) so sopds-go and `fb2-to-wav.sh` work
   unchanged; only per-chunk latency differs.

## Effort & risks

- **Effort: LARGE.** Multi-graph orchestration + an ODE sampler loop + a new stress front-end + a
  reference-audio path. Much more than a "swap the .onnx" change.
- **CPU speed risk:** slower than realtime on the M5; a 50 h book could take many hours. Mostly an
  overnight-batch concern (acceptable for audiobooks, painful for the interactive web button).
- **RUAccent-in-Rust risk:** if there's no clean Rust/subprocess path, stress handling becomes its
  own project (bad Russian stress = the classic tell of low-quality Russian TTS).
- **License:** CC-BY-NC — OK for personal listening, **must not** ship generated audio publicly.

## Recommendation

Worth a **prototype spike** (step 1) before any Rust: verify the ONNX I/O and, more importantly,
listen to whether the Russian finetune is clearly better than `ru_RU-irina-medium` *on our
hardware/speed budget*. If the quality jump justifies the large integration + slow synth, proceed;
otherwise prefer [002 — train a Piper-high ru voice](002-piper-high-ru-uk-training.md), which is
zero-integration.

## Open questions

- Exact ONNX tensor signatures of the current `DakeQQ/F5-TTS-ONNX` export (verify per-file; the
  `speed` input dtype is reportedly inconsistent across exports).
- RUAccent availability off-PyTorch (Rust crate? CLI? rules port?).
- NFE step count vs quality vs speed on CPU.
- Real RTF on M5 Pro (CPU) and GTX 1070.
