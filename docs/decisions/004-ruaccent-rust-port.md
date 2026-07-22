# 004 — Port RUAccent (Russian stress) to native Rust

**Status:** in progress (Phase 1 landed) · **Date:** 2026-07-22 · **Branch:** `ruaccent-rs`
**Supersedes (in effect):** the Python/Nix RUAccent runtime from [003-…]/Rev 96 (`f5-bridge/flake.nix`)

## Context

The auto-F5 audiobook pipeline is native everywhere **except stress placement**: `fb2-to-f5.sh`
shells out to Python RUAccent (`ruaccent_batch.py`, run via `$RUPY`). That one dependency caused a
night of pain — RUPY/F5PY export traps, a per-machine `token_type_ids` ONNX-signature mismatch, a
koziev FOD, nixpkgs-version collisions, gitignore/lock churn, and a combined-devShell yak-shave.
Synth (`sopds-tts-rs`) already runs ONNX natively via `ort`. Porting stress to Rust deletes the whole
Python/Nix stress stack: no venv, no `$RUPY`, no HF runtime downloads, no env layering — one native
binary does stress **and** synth.

## Decision

Port RUAccent's **stress pipeline** to Rust inside `sopds-tts-rs`, targeting **bit-exact** output vs
Python on a corpus.

Bit-exactness is realistic because the two heavy components are the *same underlying libraries*:
- **`ort`** binds the same onnxruntime C++ lib Python's `onnxruntime` uses → identical model floats
  for identical inputs (softmax/argmax/0.55 threshold, NER logits, omograph pair-classification).
- **`tokenizers`** is the same HuggingFace lib, loading the same `tokenizer.json` → identical
  ids/offsets/special-mask.

So all parity risk lives in the **pure-Rust logic we author**: the tokenizer regex, the razdel
sentence-split port, NER subword→word aggregation, the omograph/yo/accent orchestration, and
`delete_spaces_before_punc`/`fix_capital`. Get those byte-for-byte and parity follows.

### Key scoping win

RUAccent loads `RuleEngine`/**koziev** (rupostagger + rulemma, CRF POS/lemmatization — the hardest
thing to port) in `load()` but **never calls it** in the stress path (`process_all_internal`). So the
port needs **zero CRF, zero lemmatization, zero koziev** — just tokenizers + 4 ONNX models + dicts +
text preprocessing. Huge scope reduction.

## The algorithm being replicated (`process_all_internal`)

1. `normalize` — drop chars outside RUAccent's allowed set.
2. `split_by_sentences` — razdel `sentenize` (Phase 3, faithful port).
3. per sentence `split_by_words` — regex `\w*(?:\+\w+)*|[^\w\s]+` on lowercased text, `" - "→" ~ "`;
   returns `words[]` + interleaved separators to rejoin losslessly.
4. `stress_usage_model` (NER) → per-word STRESS/NO.
5. `_process_yo` — `yo_homograph_model` (NER) + `yo_words`/`yo_homographs` dicts (`fix_capital`).
6. `_process_omographs` — `omograph_model` sequence-pair classify per homograph → winning variant.
7. `_process_accent` — `accents` dict; miss + >1 vowel + no punct → `accent_model.put_accent`
   (char-level, CharTokenizer, `+` when label∉{NO,STRESS_SECONDARY} & score≥0.55); `letters_accent`.
8. rejoin + `delete_spaces_before_punc`, concat sentences.

## Phasing

- **Phase 1 (DONE):** module scaffold + Cargo deps (`flate2`, `regex`); `dicts.rs` (gz-json dicts);
  `preprocess.rs` (`normalize`, `split_by_words`, `delete_spaces_before_punc`, `fix_capital`,
  `transfer_plus`, `count_vowels`, `has_punctuation`); dict-only `_process_accent`. ≈ `tiny_mode`
  minus neural. `cargo check` green; unit + real-dict integration tests pass.
- **Phase 2 (DONE):** `char_tok.rs` (CharTokenizer), `tok_bert.rs` (`tokenizers` wrapper: single/pair/
  batch), `ner.rs` (shared NER decode), `models.rs` (4 `ort` sessions). Full `process_all` orchestration
  in `mod.rs` (`_process_yo`/`_process_omographs`/`_process_accent`). Every model verified **bit-exact**
  vs Python (put_accent, stress/yo NER labels, and 15 full-sentence `process_all` cases incl. multi-
  homograph and same-word-twice disambiguation). No `ndarray` needed — `ort` tensors are built from
  `Vec + shape` directly. Sentence split still naive (Phase 3).
- **Phase 3 (DONE):** faithful razdel `sentenize` port (`razdel.rs`) — the delimiter regex, all 11
  ordered JOIN rules, the sokr/pair-sokr/initials sets, the 10-char windows, `segment`, and
  `find_substrings`, plus RUAccent's `split_by_sentences` reconstruction on top. Bit-exact vs Python on
  17 cases (abbreviations, initials, paired sokr, quotes, bullet lists, ellipsis, `?!`, smileys,
  brackets, roman-numeral chapters, dash, newline gaps) and on multi-sentence `process_all`.
- **Phase 4 (subcommand DONE; deletion pending):** `sopds-tts-rs stress [--home DIR] [--fix FILE]
  [--dump-homographs FILE]` — a stdin→stdout drop-in for `ruaccent_batch.py` (per-line, `…`↔`...`
  swap, blank passthrough, error fallback, `--fix` yo/replace, `--dump-homographs`). Corpus parity
  harness (300 real book paragraphs): **9774 / 9780 words identical (99.94%)**. The residual **6
  words** are the onnxruntime-version limit below, not port defects. Pointing `fb2-to-f5.sh` at the
  binary + deleting RUPY / the nix ruaccent-python / `f5-bridge/flake.nix` / `ruaccent_batch.py` is
  gated on Dmitry's call re: the residual.

## Production parity: 0 diffs on Fedya (Phase 4)

On **dvg-fedya** (production), where both sides use the **same onnxruntime 1.22.0 on CPU**, the Rust
`stress` is **bit-exact vs Python `ruaccent_batch.py` — 0 diffs across all 8642 lines** of a real book
(Stephen King, "11/22/63"). Two fixes closed the gap the mac run showed: (1) the stress models now run
on **CPU** on every platform (`load_session`) — matching Python's `device="CPU"` and removing CPU-vs-CUDA
float divergence; (2) `normalize` now uses RUAccent's **exact** quote/apostrophe class (it had kept the
straight `"` and curly `‘ ’`, which Python strips — surfaced on `raison d'etre` / `кат'ликом`). So the
"parity ceiling" below is a **mac-only dev artifact** (pip onnxruntime 1.27 vs ort's 1.22); on the
machine that actually generates audiobooks, parity is perfect.

## Parity ceiling: onnxruntime build differences (Phase 4, mac dev only)

Bit-exactness is bounded by the **onnxruntime build**, not the port. Python RUAccent on mac uses pip
`onnxruntime` **1.27.0**; Rust `ort` 2.0.0-rc.10 bundles onnxruntime **1.22.0**. The two produce
slightly different float logits for the same model + identical inputs (verified: tokenizer ids are
byte-identical, but e.g. the accent model's per-char softmax at one position reads 0.5008 under 1.27
vs 0.5719 under 1.22). Graph-optimization level makes no difference (tested `Disable` — same output),
so it is purely the version. This matters **only** at the accent model's hard `score ≥ 0.55` threshold:
when a borderline char crosses it, one word gains/loses a secondary `+`. All 6 diffs in the 300-line
corpus are exactly this — OOV proper nouns absent from every dict (Крулеву, Шафлярах, Тынец, Тыньца) —
audibly negligible in synthesis. The NER/omograph models are unaffected (argmax is robust to
sub-threshold float jitter, which is why 294/300 lines match exactly). True 0-diff parity would need
the *same* onnxruntime build on both sides — impractical (pip 1.27 vs ort 1.22 vs Fedya's nix CUDA
onnxruntime, all different) — so the port is **algorithmically** exact with a documented, inaudible
version-noise floor.

## Notable parity finding (Phase 2)

Bit-exact parity means the port reproduces the **model's mistakes** too. E.g. the turbo2 omograph model
mis-stresses sentence-initial capitalised "Белки" (proteins → should be `белк+и`) as `б+елки`
(squirrels) in every context — a known model limitation, not a port bug (the `omographs` dict carries
both variants). The parity tests assert the Python output (`Б+елки`), NOT the linguistically correct
form. Fixing such cases is a *separate, post-parity* concern (RUAccent's `custom_dict`/`custom_homographs`
overrides, or a context rule) and would deliberately diverge from Python — so it is out of scope for the
port itself.

## Notable parity finding (Phase 1)

RUAccent's tokenizer regex `\w*(?:\+\w+)*|[^\w\s]+` relies on CPython 3.7+ `finditer` semantics: after
`\w*` matches empty, `must_advance` forces the engine to reach `[^\w\s]+`, so bare punctuation ("!!!",
a trailing ".") is captured. Rust's `regex::find_iter` has no `must_advance` — it reports the empty
`\w*` and never tries the second alternative, dropping punctuation. Fix: match on the **non-empty
subset** regex `\w+(?:\+\w+)*|(?:\+\w+)+|[^\w\s]+`, verified token-for-token against Python `finditer`
(= Python's `words_mask` output). Separators are rebuilt from gaps between spans. Pinned by the
`split_matches_python` unit test.

## Alternatives considered

- **Go port** — no first-class onnxruntime binding of the quality `ort` gives; would fight the same
  tokenizer parity battle without the same-library guarantee. Rust reuses the existing `ort`/`f5.rs`
  integration.
- **Heuristic sentence splitter** instead of a faithful razdel port — rejected: any boundary
  difference breaks bit-exactness (stress_usage/yo NER run per sentence).
- **Keep Python, just harden the Nix** — rejected: the whole point is to delete the Python/Nix stress
  stack, not stabilize it.

## Out of scope

koziev/rupostagger/rulemma/RuleEngine (unused by stress); non-`turbo2` omograph variants; poetry
models; English/Ukrainian stress (en needs none; uk gets its own tool).
