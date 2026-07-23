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
| `sopds fb2-extract` | — | (native Go) FB2 → per-unit narration text: 2-level section **or** flat bold-heading split, PARTS/COMBINE selector, spoken headings, inline footnotes |
| `reviewer/` | — | Go web tool to proofread the RUAccent stress before synthesis (two-pane, ё-homograph flags) |
| `fb2-to-f5.sh` | — | orchestrator: split by part → chunk → stress → F5 daemons → per-part MP3 |
| `merge_ellipsis.py` | — | graft `…` back into already-stressed text (RUAccent strips it — never re-stress) |

**Both halves now run native (zero Python for stress/synth).** Stress is the `sopds-tts-rs stress`
subcommand (a bit-exact Rust port of RUAccent — see `docs/decisions/004-ruaccent-rust-port.md`); synth
is native F5 via `sopds-tts-rs <model_dir>`. `fb2-to-f5.sh` drives both through the one `sopds-tts-rs`
binary (`STRESSBIN`/`F5BIN`). The RUAccent *models* still live at `RUACCENT_HOME` (~/.cache/ruaccent),
provisioned out-of-band. (`ruaccent_batch.py` + the `f5-bridge` nix flake were removed with the port.)

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

Wrap the command in `nix shell nixpkgs#libxml2 nixpkgs#gawk nixpkgs#ffmpeg nixpkgs#gnused
nixpkgs#coreutils -c …` (or have those on PATH). Common env: `NFE` (16 good · 8 fast/rough ·
32 best) · `WORKERS` (parallel daemons) · `MAXCHARS` (250) · `DEVICE` (cpu|cuda) · `PARTS="4"`
(subset) · `REMOVE_SILENCE=1` · `FIX=corrections.json` · `REF/REF_TEXT/CKPT/VOCAB/F5_HOME`.

**One-shot (no proofreading):**
```bash
FIX=corrections.json REMOVE_SILENCE=1 NFE=16 WORKERS=3 ./fb2-to-f5.sh book.fb2 ./out
```

**Two-phase (proofread the stress — the only path to perfect Russian stress):**
```bash
# 1) stress → reviewable text, no GPU
MODE=stress FIX=corrections.json ./fb2-to-f5.sh book.fb2 ./out
#    out/review/NN_<title>.txt   one stressed chunk per line — EDIT to fix any stress
#    out/review/_check-yo.tsv    short list of ambiguous ё-homographs to eyeball (берет, десны…)
# 2) synth from the (edited) text
MODE=synth REMOVE_SILENCE=1 NFE=32 WORKERS=3 DEVICE=cuda ./fb2-to-f5.sh book.fb2 ./out
```

Output: `out/NN_<title>.mp3`, **one per chapter** (see below).

### Structure & footnotes (`sopds fb2-extract`, native Go — `internal/narrate`)

The native extractor handles **two book shapes**:
- **Sectioned** (the common case): the main body nests `<section>/<section>` = part → chapter.
- **Flat** (rare, e.g. "11/22/63"): one `<section>` whose parts/chapters are **bold heading
  paragraphs** (`<p><strong>Часть N…/Раздел N</strong></p>`) in the paragraph stream.

`PARTS` selects units in **two levels** — `P` (whole part) · `P1-P2` · `P:S` (chapter S of part P) ·
`P:S1-S2`, space-separated — and `COMBINE` sets MP3 granularity: `1` = one MP3 per part (chapters
joined), `2` = one per chapter. Each chapter opens with a **spoken heading**: the first chapter of a
part is prefixed with the part title (`Книга первая. Дети вора Самуила. Глава первая.`), the rest just
`Глава вторая.` — bare-numeric chapter titles are voiced as feminine ordinals (F5 would read a lone
digit as a cardinal). Units are keyed `NN` (whole part) / `NN.MM` (chapter).

**Footnotes** (`<body name="notes">`) are read **inline** where referenced, as `Примечание. <text>`
(the note's own leading number dropped), bracketed by a hard chunk boundary so each note becomes its
own chunk — never packed mid-narrative. The review `.txt` already shows exactly what will be spoken.
(This replaced the old `fb2_extract.py` + the shell `xmllint/awk` extraction — the pipeline has no
Python for extraction/stress/synth.)

### Native synth engine (no Python)

`ENGINE=native` runs the SYNTH phase through **`sopds-tts-rs`** (Rust/`ort`) instead of
`f5_daemon.py` — same NDJSON protocol, no `f5env`. The stress phase is unchanged (still RUAccent).

```bash
# build once (CUDA on a GPU box; CPU build works but is ~80 s/chunk — synth on a GPU)
( cd ../sopds-tts-rs && cargo build --release )
# F5MODEL = a dir with the 3 exported graphs + vocab.txt + ref.wav + ref.txt (see docs/decisions/001)
MODE=synth ENGINE=native F5MODEL=/path/to/f5model WORKERS=1 ./fb2-to-f5.sh book.fb2 ./out
```

`native` **ignores** `NFE`/`DEVICE`/`REF*`/`CKPT`/`VOCAB`/`REMOVE_SILENCE` — they're baked into
`F5MODEL` and the export (NFE is fixed at 32). `F5BIN` overrides the binary path; `THREADS` caps
ORT intra-op threads when `WORKERS>1`. CUDA vs CPU is a compile-time choice (CUDA on Linux, CPU on
macOS), so build the binary on the machine you'll synth on.

**Stress reviewer (GUI for step 1).** Editing the `.txt` by hand works; the reviewer makes it
pleasant. Two-pane, line-aligned: raw reference on the left, editable stressed text on the right,
with `+` rendered as real acute accents. Rare, genuinely-ambiguous **ё-homographs** (`берёте`,
`нёбо`, `колёса`, `самоё`…) are flagged red and reachable with **`n`** / the *↓ флаг* button;
frequent near-always-ё forms (`всё`, `чём`, `нём`) are shown subtly but not counted. Saves back to
the `.txt` (one-time `.orig` backup), so it feeds `MODE=synth` directly.

```bash
cd reviewer && go build -o /tmp/f5-reviewer . && /tmp/f5-reviewer -dir ~/f5-review   # → http://127.0.0.1:8765
```

**Why two-phase.** Russian ё/stress homographs are context-dependent (`берет` noun vs `берёт`
verb; `десны` gen-sg vs `дёсны` plural). RUAccent's neural model gets most right but errs on
some, and a global override in `corrections.json` (`{"yo":{"берет":"берет"}}`) can only force
*one* sense — safe only where a word is single-sense in the book. Everything else is fixed by
editing the review text once; synthesis then runs on the final text.

**Clean the voice reference.** F5 clones the reference's noise floor. Denoise your clip first —
it lowers pause hiss a lot:
```bash
ffmpeg -i ref.wav -af "afftdn=nf=-25,highpass=f=70,lowpass=f=8500" -ac 1 -ar 24000 ref_clean.wav
```

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
