# f5-bridge — future ideas

Collecting UX/architecture ideas for the F5 audiobook pipeline (and, eventually, the native
Rust port — see ../docs/decisions/001).

## Two-pane stress-review editor (reuse fbe-go)

Editing the `review/NN_*.txt` files by hand works but is clumsy. A GUI would make proofreading
the RUAccent stress/ё fast and pleasant:

- **Left pane** — raw extracted text (reference, read-only), aligned by chunk/paragraph.
- **Right pane** — the stressed + ё text (editable), same alignment.
- **Highlighting** — ё-restorations and stress on genuine **homographs** (from `_check-yo`) coloured
  so the eye lands on the suspicious ones (небо/нёбо, лет/лёт, берет/берёт, колеса/колёса…). A diff
  view of exactly what RUAccent changed.
- **Navigation** — "next flag" (Tab), click-to-jump; later, play a chunk's synthesized audio inline
  to verify by ear.
- **Save** → writes the corrected review text → feeds `MODE=synth` directly.

**Reuse from `fbe-go`** (../../fbe-go): it already does FB2 parsing + round-trip + XSD validation on
a Wails (Go) + Svelte + ProseMirror stack. Take its FB2 extraction and editor components — ideally
add this as a **panel/mode inside fbe-go** (it already opens FB2 books), or a sibling app sharing its
FB2/editor libraries.

*Rationale:* Russian ё/stress homographs are context-dependent and can't be fully auto-resolved
(PROGRESS/decisions), so a human-in-the-loop review is the only path to perfect narration — make that
loop as frictionless as possible.

## TODO: native (Rust) RUAccent — the last Python piece [option B]

Synthesis is already native (`sopds-tts-rs` F5 engine + daemon); only RUAccent stress remains
Python. It runs **once** per book (cheap, decoupled from synthesis), so this is low-urgency — but
it's the final step to zero-Python. Decision (2026-07-13): keep **A** (RUAccent Python, current
`ruaccent_batch.py`) for now; **B** parked here as a TODO.

RUAccent is not one graph — it's a mini-pipeline: 3 ONNX models (`accentuation`, stress/usage
predictor, ё/omograph resolver), a stress dictionary + rule engine, a char tokenizer, and razdel
word segmentation. A full Rust port = load those ONNX via `ort` + port the dict lookups + rule
engine + tokenizer + segmenter. Watch the CRLF/encoding gotcha on every dict/lexicon file
(see the `rust-crlf-vocab-empty-map` note — use `.lines()`, assume CRLF).

**Cheaper middle path (option C):** Rust dictionary-only stressor (covers most words), skip the
neural omograph model — its calls are imperfect and get proofread anyway. Loses auto-homograph
disambiguation (small, since review catches it), kills the biggest Python dep. Probably the
right first cut.

## (more ideas go here)
