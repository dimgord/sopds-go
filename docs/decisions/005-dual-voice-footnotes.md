# 005 — Dual-voice footnotes, airport-bell chimes, and the `[N]`/КОММЕНТАРИИ note convention

Status: accepted (Rev 113, 1.9.0) · Supersedes nothing · Related: [001](001-f5-tts-onnx-integration.md), [004](004-ruaccent-rust-port.md)

## Context

The auto-F5 audiobook pipeline inlines a book's footnotes into the narration stream so they're read at
their reference site. Two gaps remained:

1. **Notes were always read in the main narration voice.** The worker already exposed a
   `notes_model` (→ `F5MODEL_NOTES`) — ru "luka" was configured — but `fb2-to-f5.sh` never consumed it,
   so the second voice was dead. Footnotes were indistinguishable from the surrounding prose.
2. **Only one footnote convention was handled**: FB2 `<a href="#id">` markers with a
   `<body name="notes">`. A large class of Russian books (e.g. "11/22/63", 724 notes) instead use
   **plain-text `[N]` markers** in the prose and a bold-headed **"КОММЕНТАРИИ"** section of `[N] text`
   paragraphs. Those books had their entire comment list read aloud at the end and no inlining.

## Decision

### Dual-voice synth (route footnote chunks to a second voice)

A footnote is inlined SEP-bracketed (`SEP … SEP`) so the chunker isolates it. Because `resolveNotes`
always emits a **balanced** pair, splitting a unit's text on `SEP` puts narration on **even** segment
indices and notes on **odd** ones — so note-ness is knowable per chunk with zero extra bookkeeping.

- `narrate.ChunkMask` returns the chunks **plus** a `[]bool` note-mask.
- `Extract` writes a `<ID>_<safe>.notes` **bitstring sidecar** (one `0/1` per chunk). It aligns with the
  stressed `.txt` because stressing is line-preserving (`none` = copy, `ruaccent` = one line out per line
  in). Written only when the unit has a note; absent ⇒ all narration.
- `ndjson-reqs --notes-file` reads the sidecar and routes flagged chunks to a **second NDJSON stream**,
  naming their wavs `p<id>_c<NNNNN>.note.wav`. **The `c` counter is shared across both streams**, so the
  zero-padded number still orders note and narration wavs correctly in the per-part concat — the join is
  voice-agnostic.
- `fb2-to-f5.sh` runs a second F5 daemon on `F5MODEL_NOTES` for the notes stream (when set; else one
  stream, one voice — prior behavior). A line-count guard warns and falls back to the main voice if the
  operator's manual edits desync the mask from the `.txt`.

Why a filename marker (`.note.wav`) rather than a manifest: the join already globs+sorts wavs; a
self-describing filename needs no side-channel and survives the daemon writing files in any order.

### Airport-bell chimes around note runs

Each contiguous run of `.note.wav` is bracketed by a warm two-tone **decaying bell** — falling A5→E5
"пім-пуум" in, rising E5→A5 out. This is the sound from the old Python pipeline (commit `beedbae`):
`sine → volume='exp(-k*t)' → concat → lowpass=6500 + aecho + volume`. A brief interim implementation used
flat sine tones and sounded "midi"; the decaying/echoed bell is the accepted sound. Bells are read from
`$F5_HOME/chimes/{chime_in,chime_out}.wav` (persisted, swappable for a real airport sample) and
regenerated into the work dir only if absent. `CHIME=0` disables; `CHIME_IN`/`CHIME_OUT` override.

Known tradeoff: heavily-annotated books ring often (11/22/63 ≈ 724 note runs). Accepted — the operator
judged it worth the audible signposting; `CHIME=0` is the escape hatch per book.

### The `[N]` / КОММЕНТАРИИ convention

`parseBracketNotes(mb)` walks the main body's paragraphs in document order and finds a **bold heading**
matching `КОММЕНТАРИИ|ПРИМЕЧАНИЯ|СНОСКИ|…` **that is actually followed by `[N] text` definitions** (so a
chapter merely *titled* "Примечания" is not mistaken for the notes region). It returns id→text and a set
of nodes to drop from narration. A new `node.skip` flag — honored in every paragraph collector
(`flatParts`, `paragraphsOf`, the sectioned-intro path) — removes the comment list from the spoken output.
`resolveNotes` gains a `bracketMode` (set by `Extract` only when such notes were parsed) that inlines each
in-text `[N]` whose number is a known note; unknown `[N]` is left as literal text (it may be `[sic]`, a
quote, etc.). The href-marker path and the unit tests are unchanged when `bracketMode` is off.

Both conventions feed the **same** `notes` map and the same SEP-inlining, so dual-voice + chimes work
identically regardless of how the book encodes its footnotes.

## Consequences

- ru "luka" and en (operator's voice vs the F5 sample) footnote voices are finally audible; adding a
  language is still just a `languages.<lang>` block (`f5_model`, `notes_model`, `stress`, `note_prefix`).
- Voice assets and chime wavs live under f5-spike and deploy to the GPU host separately from this
  code-only repo (same as the F5 models).
- The mask/`.note.wav`/shared-counter design keeps the join and the review-gate resume untouched; the
  `.notes` sidecar rides along in the persisted `.tts-review/<book_id>/` staging dir.

## Alternatives considered

- **Prefix-detection at synth time** (route chunks whose text starts with "Примечание.") — fails for
  multi-chunk notes (only the first chunk carries the prefix). The SEP-parity mask is exact.
- **A separate notes file interleaved at join time by position** — needs the same ordering info the
  shared `c` counter already provides; rejected as redundant.
- **Committing chime wavs to the repo** — kept them under `$F5_HOME/chimes` instead, beside the voice
  assets, so a real airport sample can be dropped in without a code change or a binary in git.
