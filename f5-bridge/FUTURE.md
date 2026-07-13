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

## (more ideas go here)
