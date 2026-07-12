# TTS beyond Piper for Russian/Ukrainian — research notes

**Date:** 2026-07-12 · **For:** [001 F5](decisions/001-f5-tts-onnx-integration.md) ·
[002 Piper-high](decisions/002-piper-high-ru-uk-training.md)

**Bottom line:** for an ONNX-Runtime-only Rust pipeline there is **no clearly-better-than-Piper-medium
drop-in for Russian/Ukrainian.** The genuinely higher-quality options each break one of our hard
constraints (ONNX-runnable / permissive license / clean integration). Realistic wins: **train a
Piper-high ru/uk** (zero integration) and/or an **F5-TTS-Russian** spike (real quality, large work).

Licensing note: Dmitry's use is **personal/self-hosted (non-commercial)**, so CC-BY-NC *weights* are
fine to use; NC only blocks redistributing the model or the generated audio.

## Options scored against our constraints

| Model | ru / uk | Beats Piper-medium? | License (weights) | ONNX under `ort` (no PyTorch)? | Integration |
|---|---|---|---|---|---|
| **Piper "high" ru/uk (train it)** | ✅ / ✅ | yes (bigger model) | MIT (our data) | ✅ native — same `[input,input_lengths,scales]`, 22050 | **none** (drop-in) + training |
| **robinhad/ukrainian-tts** | uk only | yes for uk (real stress) | MIT (newer; older GPLv3) | ✅ via `espnet_onnx` | medium (new G2P) |
| **Silero v5** | ✅ / ✅ | yes (best free ru) | CC-BY-NC (MIT only for `v5_cis_base*`) | ❌ **PyTorch only, no ONNX** | not viable in `ort` |
| **F5-TTS-Russian** | ✅ ru | likely yes | CC-BY-NC (code MIT) | ✅ `DakeQQ/F5-TTS-ONNX` | large (3 graphs, ref-audio, ODE) |
| **MMS-TTS rus/ukr (Meta)** | ✅ / ✅ | no (16 kHz, robotic) | CC-BY-NC | ✅ but char tokenizer, no `scales` | downgrade |
| **XTTS-v2 (Coqui)** | ⚠️ ru poor | no ("much worse than expected") | non-commercial CPML, licensor defunct | ❌ autoregressive | N/A |
| **Kokoro-82M** | ❌ **none** | — (great for en) | Apache-2.0 | ✅ clean | N/A for ru/uk |
| **StyleTTS2** | ❌ no ru/uk model | — | MIT | ✅ | large (needs Cyrillic training) |

## Key facts (verified)

- **Kokoro v1.0** supports exactly 8 languages (en·es·fr·hi·it·ja·pt-BR·zh) — **no Russian/Ukrainian**;
  misaki G2P covers only en/ja/ko/zh/vi. Any "Kokoro Russian" repo is a from-scratch DIY, not a
  backend. *(hexgrad/Kokoro-82M VOICES.md; hexgrad/misaki; kokoro issue #76.)*
- **Silero** is the best-regarded *free* Russian TTS (auto stress + homograph resolution) but ships
  **only as PyTorch `.pt` (torch.package)** — no official TTS ONNX (conversion issue #283 open). Would
  require libtorch (`tch`), abandoning our `ort`-only design. *(snakers4/silero-models, models.yml.)*
- **F5-TTS** is the **only** higher-tier model that is both Russian-capable and fully ONNX-runnable:
  `DakeQQ/F5-TTS-ONNX` (3-graph export, CPU/GPU builds, MLX port); Russian finetune
  `Misha24-10/F5-TTS_RUSSIAN` (~5000 h, RUAccent stress). 336M flow-matching DiT, needs a reference
  clip, slower-than-realtime on CPU, CC-BY-NC. *(DakeQQ/F5-TTS-ONNX; SWivid/F5-TTS.)*
- **MMS-TTS rus/ukr** is ONNX-exportable but a **downgrade**: 16 kHz, single-speaker, raw-Cyrillic char
  tokenizer (no espeak IPA, no `scales`), CC-BY-NC. *(facebook/mms-tts-rus config.json.)*
- **Piper baseline:** `ru_RU` has only medium voices (`denis`/`dmitri`/`irina`/`ruslan`); `uk_UA` has
  `lada` (x_low) + `ukrainian_tts` (medium). No `high` for either. *(rhasspy/piper VOICES.md.)*

## Kokoro ONNX I/O (reference only — no ru/uk, so not for us)

If we ever add a non-ru/uk high-quality English voice: inputs `input_ids` int `(1,≤512)` (older
exports name it `tokens`), `style` f32 `[1,256]` **indexed by token length** (`voices[len(tokens)]`),
`speed` (dtype inconsistent — read per file); output f32 waveform @ 24 kHz. Canonical export
`onnx-community/Kokoro-82M-ONNX`. espeak-ng phonemes usable via the fixed `config.json` vocab.

## Sources

- https://huggingface.co/hexgrad/Kokoro-82M/blob/main/VOICES.md · https://github.com/hexgrad/misaki
- https://github.com/snakers4/silero-models · https://github.com/snakers4/silero-models/issues/283
- https://github.com/DakeQQ/F5-TTS-ONNX · https://huggingface.co/Misha24-10/F5-TTS_RUSSIAN
- https://huggingface.co/facebook/mms-tts-rus · https://github.com/robinhad/ukrainian-tts
- https://github.com/rhasspy/piper/blob/master/VOICES.md
- https://alphacephei.com/nsh/2024/07/12/russian-tts.html (independent ru TTS benchmark)
