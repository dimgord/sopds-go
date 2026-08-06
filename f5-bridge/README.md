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

**Pauses, footnotes and the join.** The chunker decides *where* a pause belongs; the joiner decides
*how long* it is. Every chunk therefore gets a deterministic silence after it, sized by its own
trailing mark — and because the pause carries the prosody, the chunker is free to split on a comma
without the split being heard as a stumble.

```bash
PAUSE_DOT=0.5 PAUSE_COMMA=0.25 PAUSE_DASH=0.7 PAUSE_ELLIPSIS=0.8   # per trailing mark
PAUSE_SEMI=0.4 PAUSE_COLON=0.3 PAUSE_DEFAULT=0.15
PUNCT_PAUSE=0            # off ⇒ one flat SENT_PAUSE (0.30) at sentence ends only
PUNCT_CLEAN=0            # off ⇒ keep a trailing '…' / spaced em dash in the synthesized text
TITLE_PAUSE=1.5          # brackets a heading on BOTH sides (K per part from _titlecount.tsv)
NOTE_PAUSE=0.3           # after a footnote's closing chime
TRIM_SILENCE=0           # off ⇒ keep each chunk's own leading/trailing silence
WORK_DIR=/path JOIN_ONLY=1   # re-join kept wavs with different pauses — no re-synthesis
```

`PUNCT_CLEAN` rewrites a trailing `…` to `.` and drops a trailing spaced em dash **before** synthesis,
because F5 renders both poorly — while the pause is still sized from the *original* mark, so an
ellipsis keeps its longer silence. Footnotes are found from the `<id>_<safe>.notes` mask written by
`sopds fb2-extract`; a review dir proofread before that mask existed falls back to the text prefix
(`NOTE_PREFIX`, defaulting to `Примечание.` in `ruaccent` mode) and says so, rather than silently
letting the narrator read "Примечание" mid-sentence. With `NOTES_VOICES=<dir>` holding one model dir
per voice plus `manifest.txt`, each footnote gets a different voice in rotation; `F5MODEL_NOTES` is
the single-voice form. Either way the note is bracketed by the two-tone chime.

`WORK_DIR` is worth setting on any long run: pause lengths are a join-time parameter, so "1.5 s turned
out too long" becomes a two-minute `JOIN_ONLY=1` instead of a four-hour re-synthesis.

**Which GPU — and what to do about Blackwell.** The CUDA kernels live in `libonnxruntime.so`, not in
our binary, so the card you may rent is decided by the ONNX Runtime you link against. ORT **1.22 has
no kernels for `sm_120`**: on a Blackwell card (RTX PRO 6000, RTX 5090, B200) the CUDA provider fails
to register, ORT falls back to CPU **without printing anything**, and the only symptom is that a chunk
takes 34 s instead of 2.6. Anything up to Hopper (`sm_70`…`sm_90` — V100, A100, RTX 3090/A6000, 4090,
L4/L40/L40S, RTX 6000 Ada, H100/H200) just works.

Blackwell needs a newer ORT, and — because ORT versions its symbols (`VERS_1.22.0`) — swapping the
`.so` under an existing binary is **not** enough; it must be rebuilt against the new library. That
part is cheap:

```bash
curl -sSLO https://github.com/microsoft/onnxruntime/releases/download/v1.28.0/onnxruntime-linux-x64-gpu_cuda12-1.28.0.tgz
tar --no-same-owner -xzf onnxruntime-linux-x64-gpu_cuda12-1.28.0.tgz     # network volumes reject chown
export ORT_LIB_LOCATION=$PWD/onnxruntime-linux-x64-gpu_cuda12-1.28.0/lib
( cd sopds-tts-rs && cargo build --release )      # ~4 s: ort is linked, not compiled
export LD_LIBRARY_PATH=$ORT_LIB_LOCATION          # also needed at RUN time
```

Measured on one identical 12.5 s chunk: Blackwell + ORT 1.28 **3.83 s**, RTX 4090 + ORT 1.22 **4.31 s**,
A100 **6.00 s**. Blackwell is only ~12% faster per card and costs ~3× more, so it is the fallback for
when Ada cards are out of stock, not the default. Whichever card you rent, **time one chunk before
launching a run** — 34 s versus 2.6 s is the difference between a silent CPU fallback and a working
GPU, and nothing else reports it.

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
