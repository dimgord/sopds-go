# f5-bridge — F5-TTS audiobook narration (temporary Python bridge)

Narrate an FB2 book with **F5-TTS** in a **cloned voice** (yours, or any reference), with
Russian **stress** placement (RUAccent), one MP3 **per part**. Quality is well above Piper;
the cost is speed (F5 is a 336 M diffusion model — see below).

> **This is a bridge, not the destination.** F5's inference is PyTorch, so this uses Python.
> The real target is a native **Rust (`ort`)** F5 port with zero Python at synth time —
> [`docs/decisions/001-f5-tts-onnx-integration.md`](../docs/decisions/001-f5-tts-onnx-integration.md).
> When that lands, delete this directory.

## Pieces

| file | venv | role |
|------|------|------|
| `f5_daemon.py` | `f5env` | resident F5 (load once, NDJSON `{text,output}` in → WAV out) |
| `ruaccent_batch.py` | `ruaccent-env` | batch RUAccent stress (chunks in → stressed out, per-line fallback) |
| `fb2-to-f5.sh` | — | orchestrator: split by part → chunk → stress → F5 daemons → per-part MP3 |

**Two venvs on purpose.** RUAccent's ONNX omograph model needs `transformers < 5` (v5 dropped
`token_type_ids`), but `f5-tts` pulls `transformers 5.x`. They can't share a venv — the classic
Python version clash. So RUAccent lives in its own venv and the two talk via files.

## Setup (one-time, uses `uv`)

```bash
H=~/src/f5-spike            # F5_HOME
mkdir -p $H/ru-model && cd $H

# 1. F5 venv
uv venv f5env --python 3.11
uv pip install --python f5env/bin/python f5-tts openai-whisper   # whisper only to transcribe your ref

# 2. RUAccent venv (older transformers)
uv venv ruaccent-env --python 3.11
uv pip install --python ruaccent-env/bin/python ruaccent "transformers<5"

# 3. Russian model (CC-BY-NC — personal use ok) + vocab
B=https://huggingface.co/Misha24-10/F5-TTS_RUSSIAN/resolve/main
curl -sL $B/F5TTS_v1_Base_v2/model_last_inference.safetensors -o ru-model/model_v2.safetensors
curl -sL $B/F5TTS_v1_Base/vocab.txt                            -o ru-model/vocab.txt

# 4. Voice reference: a clean ~8 s mono clip + its EXACT transcript
#    (record yourself; keep it ≤~10 s and trim trailing words or F5 echoes them).
#    -> $H/ab/ref_fixed.wav  +  $H/ab/ref_fixed.txt
```

## Usage

```bash
NFE=16 WORKERS=3 nix shell nixpkgs#libxml2 nixpkgs#gawk nixpkgs#ffmpeg \
  nixpkgs#gnused nixpkgs#coreutils -c \
  ./fb2-to-f5.sh book.fb2 ./out

# env: NFE (16 good, 8 fast/rough, 32 best) · WORKERS (parallel daemons) · MAXCHARS (250)
#      DEVICE (cpu|cuda) · PARTS="4" (subset, for testing) · REF/REF_TEXT/CKPT/VOCAB/F5_HOME
```

Output: `out/NN_<title>.mp3`, one per top-level `<section>` (the book's own parts).

## Speed (measured, mac5 M5 Pro CPU)

| nfe | RTF | ~8 h book, 1 worker | 3 workers |
|-----|-----|---------------------|-----------|
| 8   | ~0.5 | ~4 h  | ~1.5 h |
| 16  | ~1.5 | ~12 h | **~4 h** |
| 32  | ~3.5 | ~28 h | ~10 h |

**GPU note:** a GTX 1070 (Pascal) is **not** faster than the M5 Pro CPU for F5 (~1.2×) and
modern PyTorch dropped `sm_61` — on that box you must pin `torch==2.4.1+cu121` and run under an
FHS (`NIXPKGS_ALLOW_UNFREE=1 nix run --impure nixpkgs#steam-run -- …`) on NixOS. A **rented
modern GPU** (RTX 4090 / A100) is the real win — RTF < 0.3 even at nfe=32.
