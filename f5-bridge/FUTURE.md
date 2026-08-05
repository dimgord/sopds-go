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

## TODO: merge the legacy pod script's join back into `fb2-to-f5.sh`

`fb2-to-f5.legacy-pod.sh` is the variant that narrated the Russian book. Going native (Go
`fb2-extract` + `ndjson-reqs`) modernised the *front* of the pipeline and quietly dropped three
things from the *back* of it, all audible:

* **`PUNCT_PAUSE`** — a pause after every chunk sized by its own trailing mark, with `PUNCT_CLEAN`
  rewriting a trailing `…` to `.` and dropping a trailing spaced em dash before synthesis. The
  maintained script has no pause at all until the 2026-08-05 `SENT_PAUSE`, which is one flat value.
* **`TRIM_SILENCE`** — trims each chunk's edges so the deterministic pause is the only gap.
* **Footnote cast voices** — the legacy script finds notes by the `"Примечание."` text prefix and
  rotates a cast (`NOTES_VOICES/manifest.txt`) through them with entry/exit chimes. The maintained
  one reads a `.notes` sidecar that only the Go extractor writes, so any hand-reviewed book from
  before that change silently loses both the voice and the chimes.

The merge wants the prefix detection as a **fallback** when no sidecar exists, and the punctuation
map in place of the single `SENT_PAUSE`. Until then, a book with an older review dir must run the
legacy script.

## TODO: pin synth workers to GPUs (multi-card pods buy wall-clock, not money)

`sopds-tts-rs` builds `CUDAExecutionProvider::default()` and reads no device variable, so every
worker lands on **device 0** — on a 3-card pod the other two idle while three daemons fight over the
first. The fix is one line in `fb2-to-f5.sh`'s worker loop, since `CUDA_VISIBLE_DEVICES` remaps
device 0 per process:

```bash
CUDA_VISIBLE_DEVICES=$((i % ${GPUS:-1})) "$F5BIN" "$F5MODEL" < "$WORK/shard_$i" &
```

Worth knowing what it does and does not buy. RunPod bills per GPU-second, so three cards for a third
of the time cost the **same money** — the only thing bought is wall-clock, which matters when someone
is waiting, or when a pod has an hourly minimum. Reference figure: 715 chunks in 31 min on one card
with `WORKERS=3`, i.e. ~2.6 s per chunk.

Deferred deliberately on the 2026-08-05 run: that run already changed the cut, the stresses and the
joiner's pauses, and adding an untested device-pinning path would have made a bad result
unattributable. Do it on a run where nothing else is new.

## (more ideas go here)
