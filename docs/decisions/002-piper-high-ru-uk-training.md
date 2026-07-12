# 002 — Train Piper "high" voices for Russian & Ukrainian

**Status:** exploratory (branch `tts-quality`) · **Date:** 2026-07-12 · **Scope:** offline training, **zero** `sopds-tts-rs` code change

## Problem

Piper's public catalog has **no `-high` voice for Russian or Ukrainian** — only:

- `ru_RU`: `denis`, `dmitri`, `irina`, `ruslan` — all **medium**
- `uk_UA`: `lada` (**x_low**), `ukrainian_tts` (**medium**)

"medium" vs "high" is model size at the same 22.05 kHz. Training our own **high** voice is the only
upgrade path that stays **100 % drop-in**: same `[input, input_lengths, scales]` ONNX contract, same
espeak-ng front-end, same 22050 Hz — the existing runner loads it with **no code change**, unlike
[001 — F5](001-f5-tts-onnx-integration.md).

## Why this is attractive

- **Integration effort: none.** Output is a `.onnx` + `.onnx.json` you drop into `models_dir`.
- **License: clean (MIT).** The voice is ours; only the training *data* license matters, and open
  datasets exist.
- **Runs everywhere we already run** — CUDA on fedya, CPU on mac5 — because it's the same VITS graph.
- Quality: a well-trained `high` beats `medium`; and for **Ukrainian**, the base is only `x_low`/
  `medium`, so the headroom is real.

The cost moves from *code* to *training* (data prep + GPU hours), which is a one-time, offline job.

## Datasets (open)

- **Russian:** **RUSLAN** (single-speaker, ~studio quality, the usual base for good ru VITS), or
  Common Voice ru for multi-speaker. `ru_RU-ruslan-medium` already derives from RUSLAN — a natural
  base to fine-tune *up* to high.
- **Ukrainian:** the open Ukrainian TTS datasets behind `robinhad/ukrainian-tts` /
  `egorsmkv/*` (LADA, Mykyta, Tetiana). `uk_UA-ukrainian_tts-medium` is from this lineage.

Verify each dataset's license before use (most are permissive/CC, but check).

## Approach

Use the official **piper-recipes / `piper-train`** flow (VITS-based). Two viable strategies:

1. **Fine-tune an existing checkpoint up to `high`** (recommended, cheaper): start from the released
   `ru_RU-ruslan-medium` (or the `ukrainian_tts` checkpoint) and continue training with the `high`
   config on the same/curated data. Far fewer GPU-hours than from scratch, and inherits a good
   phonetic base.
2. **Train `high` from scratch** on RUSLAN / the Ukrainian sets — best ceiling, most GPU time.

Pipeline (per voice):

1. Prepare dataset → LJSpeech-style `metadata.csv` + wavs (22050 Hz, mono).
2. `piper_train.preprocess` with the matching **espeak-ng voice** (`ru` / `uk`) — this is what keeps
   it drop-in with our runner (same phonemization).
3. Train the **`high`** config (bigger model) to convergence; monitor with the sample outputs.
4. `piper_train.export_onnx` → `<voice>-high.onnx` (+ copy the generated `.onnx.json`).
5. Drop into `models_dir`, point `tts.voices` at it. Done — no runner change.

## Hardware & time

- **fedya (GTX 1070, 8 GB):** can train medium/high VITS, but **slowly** — expect **days** per voice
  (and watch the 8 GB VRAM ceiling; may need a small batch size / grad accumulation).
- **Cloud GPU (rented A100/4090 for a few hours–days):** much faster; likely the pragmatic choice for
  a from-scratch `high`. Fine-tuning is cheap enough that fedya may suffice.
- mac5 is **not** a training box (no CUDA); it only *runs* the resulting ONNX.

## Effort & risks

- **Effort: MEDIUM–LARGE**, but **offline** and **zero integration**: data curation + GPU time +
  babysitting training. No risk to the running service.
- **Data-quality risk:** VITS quality is bounded by the dataset; a noisy corpus → a noisy voice.
  Single-speaker studio data (RUSLAN) is the safe bet for Russian.
- **Time/VRAM risk on fedya:** 8 GB may force from-scratch `high` off-box (cloud), though fine-tuning
  should fit.

## Recommendation

This is the **best risk-adjusted upgrade** for ru/uk: it keeps the entire pipeline unchanged and the
license clean. Suggested first step: **fine-tune `ru_RU-ruslan-medium` → high** on RUSLAN as a small
spike (cheapest, uses an existing base), evaluate against `irina`/`ruslan` medium, and if it's a
clear win, repeat for Ukrainian. Pair this with the [001 F5 spike](001-f5-tts-onnx-integration.md)
and pick whichever gives the bigger quality-per-effort win.

## Open questions

- Fine-tune-medium→high vs train-high-from-scratch: quality delta vs GPU cost.
- Dataset licenses (RUSLAN, the Ukrainian sets) for our (personal) use.
- Whether fedya's 8 GB is enough for the `high` config or we rent a GPU.
- espeak-ng `ru`/`uk` phonemization coverage vs a dedicated stressed front-end (Russian stress is the
  usual quality ceiling — a `medium`/`high` Piper voice still relies on espeak stress, which is
  imperfect; this caps how far Piper-high can go for Russian).
