# 001 — F5-TTS (Russian) ONNX backend for sopds-tts-rs

**Status:** spec verified, ready to implement (branch `tts-quality`) · **Updated:** 2026-07-12 · **Scope:** `sopds-tts-rs`

## Problem

Piper caps at **medium** quality for Russian/Ukrainian. F5-TTS is the only higher-tier model
that is Russian-capable **and** fully ONNX-runnable, so it can live in the same Rust `ort` runner
with **no Python at synth time** (the whole point). The Python bridge in `../../f5-bridge/` proves
the quality/pipeline today; this doc is the native replacement.

## What changed: the spike de-risked it

Exporting `Misha24-10/F5-TTS_RUSSIAN` (v2) to ONNX with `DakeQQ/F5-TTS-ONNX` **works** — three
graphs, and their built-in test synthesizes audio end-to-end via onnxruntime in ~20 s (CPU). Two
findings make the Rust port **much smaller than originally feared**:

1. **The flow-matching ODE sampler is *inside* the transformer graph.** Rust does not implement any
   diffusion math — it just runs graph B in a loop, threading two tensors. No Euler solver, no
   schedule.
2. **For Russian the tokenizer is a plain char-split.** `convert_char_to_pinyin` (jieba + pinyin) is
   Chinese-only; on Cyrillic its output is byte-for-byte `list(text)`. So the Rust front-end is
   punctuation-normalize → chars → vocab-index. **No jieba, no pinyin.**

So the "large ODE + tokenizer" risk is gone. What remains is plumbing three `ort` sessions.

## Verified spec (build to this)

**Constants:** sample_rate 24000 · hop 256 · nfe_step 32 · speed 1.0.

**Vocab:** `vocab.txt`, line *i* → `{line_text_without_newline: i}`; OOV → 0.

**Tokenizer (Russian):** `text = ref_text + gen_text`; translate `;→,`, curly quotes→straight;
split into Unicode chars; map each char → vocab index (i32). (Equals the Python path for Cyrillic —
verified.)

**max_duration** (i64): `ref_audio_len + int(ref_audio_len / ref_text_len * gen_text_len / speed)`
where `ref_audio_len = audio_samples // 256 + 1`, and `*_text_len = utf8_byte_len(text)` (the
`+3·chinese_pause_punc` term is 0 for Russian).

**Three ONNX graphs** (names/dtypes/shapes as exported):

| graph | inputs | outputs |
|---|---|---|
| **A `F5_Preprocess`** (68 MB) | `audio` i16 `[1,1,L]`, `text_ids` i32 `[1,T]`, `max_duration` i64 `[1]` | `noise` f32, `rope_cos_q/sin_q` `[2,16,D,64]`, `rope_cos_k/sin_k` `[2,16,64,D]`, `cat_mel_text` f32 `[1,D,612]`, `cat_mel_text_drop` f32, `ref_signal_len` i64 |
| **B `F5_Transformer`** (1.32 GB f32) | `noise` f32 `[1,D,100]`, the four ropes, `cat_mel_text`, `cat_mel_text_drop`, `time_step` i32 `[1]` | `denoised` f32 `[1,D,100]`, `time_step` i32 `[1]` |
| **C `F5_Decode`** (63 MB, vocos) | `denoised` f32 `[1,D,100]`, `ref_signal_len` i64 | `output_audio` **i16** `[1,1,gen_len]` ← WAV-ready |

**Algorithm:**
1. read ref audio → i16 mono @24k; build `text_ids`, `max_duration`.
2. run **A** → noise, 4 ropes, cat_mel_text, cat_mel_text_drop, ref_signal_len.
3. `time_step=[0]`; **loop nfe_step times:** run **B**(noise, ropes, cat_mel×2, time_step); feed
   `denoised→noise`, `time_step→time_step`. Ropes/cat_mel stay constant across the loop.
4. run **C**(final denoised, ref_signal_len) → i16 PCM → WAV. (An fp16 transformer export halves B.)

## Regenerating the ONNX (one-time, Python — build artifact only)

The graphs are ~1.4 GB (don't commit). Export venv = `f5-tts==1.1.7` + jieba/pypinyin/pydub/
omegaconf/onnx/soundfile; `DakeQQ/F5-TTS-ONNX/…/Export_F5.py --vocab_path … --f5safetensor_path
model_v2.safetensors --{preprocess,transformer,decoder}model_path … --vocosmodel_dir vocos-mel-24khz
--nfe_step 32`. It patches `f5_tts/…/dit.py` + `vocos/*` in that venv (hence a throwaway venv). Ship
the 3 `.onnx` next to the binary (or fetch at deploy).

## Rust plan (sopds-tts-rs)

- New `src/f5.rs`: `F5 { pre, transformer, decode: Session, vocab: HashMap<char,i64> }`.
- Detect model type by a marker (an `f5/` dir with the 3 graphs + `vocab.txt`) vs Piper's `.onnx.json`.
- Tokenizer + `max_duration` (trivial). Reference audio + its transcript ship alongside (voice clone).
- Run A → loop B (nfe) → C. Keep the **daemon NDJSON protocol** unchanged so `fb2-to-f5.sh`/sopds-go
  don't care. RUAccent stress still happens upstream (see below).
- CUDA on Linux / CPU on macOS via the same EP block as Piper.

## Effort & risks (revised)

- **Effort: MEDIUM** (was "large"): three sessions + a loop + a char tokenizer. The hard parts are
  in the graphs; the work is tensor plumbing + reference-audio handling.
- **RUAccent** (Russian stress) is still Python in the bridge. Native options: it's ONNX models +
  dicts — port the lookup + run its ONNX from Rust, or keep a tiny stress sidecar. Separate task;
  F5 audio works without it (just poorer stress).
- **Speed:** F5 is heavy regardless of language; GPU strongly preferred (RTF ~5–7 CPU on M5 Pro,
  ~1.2× on GTX 1070; a modern GPU is the real target). Batch, not interactive.
- **License:** F5-Russian weights CC-BY-NC — personal use only, don't redistribute model or audio.

## Open questions (small)

- fp16 vs fp32 transformer graph — quality vs size/speed on the target GPU.
- Read `nfe_step`/`hop` from ONNX metadata (written with `--onnx_add_metadata`) vs hardcode.
- Bundle the 3 graphs (1.4 GB) with the binary or fetch on first use.
