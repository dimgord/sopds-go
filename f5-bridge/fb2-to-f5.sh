#!/usr/bin/env bash
# fb2-to-f5.sh — narrate an FB2 book with F5-TTS in a cloned voice, one MP3 per part.
#
#   fb2-to-f5.sh <book.fb2> [out_dir]
#
# Three modes (env MODE):
#   MODE=stress  fb2_extract.py (part→chapter split, spoken "Глава N" headings, footnotes read
#                INLINE at the referencing sentence) -> RUAccent -> reviewable out/review/NN_<t>.txt
#                (one stressed chunk per line) + out/review/_check-yo.tsv (ё-additions to eyeball).
#                Edit those .txt files to fix any stress, then run MODE=synth.
#   MODE=synth   read out/review/NN_*.txt (as edited) -> F5 daemons -> one out/NN_<title>.mp3 each
#   MODE=all     (default) stress then synth in one pass, no review stop.
#
# One MP3 per SECOND-level <section> (chapter); a part with no chapters stays one MP3. The first
# chapter of each part is announced with the part title ("Книга первая… Глава первая"); numeric
# chapter titles are voiced as feminine ordinals. PARTS filters by top-level section (part).
#
# env: NFE=16 WORKERS=1 MAXCHARS=250 DEVICE=cpu PARTS="1 2 3 4"(subset)  REMOVE_SILENCE=1
#      FIX=corrections.json  REF/REF_TEXT/CKPT/VOCAB/F5_HOME
#      ENGINE=python|native  — synth backend. native = sopds-tts-rs (Rust/ort, no Python):
#         F5BIN=<repo>/sopds-tts-rs/target/release/sopds-tts-rs  F5MODEL=<dir: 3 onnx+vocab+ref>
#         native ignores NFE/DEVICE/REF*/CKPT/VOCAB/REMOVE_SILENCE (baked into the model dir; NFE
#         fixed at 32 by the export). Build CUDA on a GPU box — CPU is ~80s/chunk (use a GPU).
#      Footnote voice(s): F5MODEL_NOTES=<dir> (single 2nd voice) or NOTES_VOICES=<dir of voice
#         model-dirs + manifest.txt> (cast: a different voice per footnote). NOTE_CHIME=<wav>
#         (default: built-in A5→E5 bell) plays before each note; NOTE_PAUSE=0.3 silence after.
#
# The STRESS half is still Python (RUAccent); the SYNTH half goes native with ENGINE=native.
# See docs/decisions/001; FUTURE.md option B tracks porting RUAccent too.
set -euo pipefail

FB2=${1:?usage: fb2-to-f5.sh <book.fb2> [out_dir]}
OUT=${2:-./f5-book}
F5_HOME=${F5_HOME:-~/src/f5-spike}
REF=${REF:-$F5_HOME/ab/ref_clean.wav}
REF_TEXT=${REF_TEXT:-$F5_HOME/ab/ref_fixed.txt}
CKPT=${CKPT:-$F5_HOME/ru-model/model_v2.safetensors}
VOCAB=${VOCAB:-$F5_HOME/ru-model/vocab.txt}
NFE=${NFE:-16}; WORKERS=${WORKERS:-1}; MAXCHARS=${MAXCHARS:-250}; DEVICE=${DEVICE:-cpu}
MODE=${MODE:-all}; PARTS=${PARTS:-}; FIX=${FIX:-}
F5PY="$F5_HOME/f5env/bin/python"; RUPY="$F5_HOME/ruaccent-env/bin/python"
ENGINE=${ENGINE:-python}   # python (f5_daemon.py) | native (sopds-tts-rs, Rust/ort — no Python)
REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
F5BIN=${F5BIN:-$REPO/sopds-tts-rs/target/release/sopds-tts-rs}
F5MODEL=${F5MODEL:-/tmp/f5model}
F5MODEL_NOTES=${F5MODEL_NOTES:-/tmp/f5model-notes}  # 2nd voice for footnotes (ENGINE=native); the
# note chunks (each starts with "Примечание.") are routed here for a distinct "dry footnote" voice
NOTES_VOICES=${NOTES_VOICES:-/tmp/f5voices}  # optional cast: a dir of voice model-dirs + manifest.txt.
# If present, each footnote gets a different voice (rotated in order) instead of the single F5MODEL_NOTES
REVIEW="$OUT/review"
mkdir -p "$OUT" "$REVIEW"
# ---- STRESS phase: extract chapters (part→chapter split, spoken headings, inline notes) ------
if [ "$MODE" = stress ] || [ "$MODE" = all ]; then
  echo "→ extracting chapters + stressing (chars≤$MAXCHARS${PARTS:+, parts $PARTS})"
  rm -f "$REVIEW"/[0-9]*.raw.txt "$REVIEW"/[0-9]*.txt   # drop stale units from a prior split
  python3 "$F5_HOME/fb2_extract.py" "$FB2" "$REVIEW" "$MAXCHARS" "${PARTS:-}"
  while IFS=$'\t' read -r pp safe title; do
    "$RUPY" "$F5_HOME/ruaccent_batch.py" ${FIX:+--fix "$FIX"} \
      < "$REVIEW/${pp}_${safe}.raw.txt" > "$REVIEW/${pp}_${safe}.txt" 2>>"$REVIEW/_ruaccent.log"
    echo "  ✓ $pp $title ($(wc -l < "$REVIEW/${pp}_${safe}.txt") chunks)"
  done < "$REVIEW/_titles.tsv"
  # Ambiguous-homograph report: only flag ё-restorations on genuine homographs (берет, десны, …),
  # not the always-ё words (ещё, всё, её). These are the ones worth eyeballing in the review text.
  "$RUPY" "$F5_HOME/ruaccent_batch.py" --dump-homographs "$REVIEW/_homographs.txt" </dev/null 2>/dev/null || true
  # Dict-based reviewer flags from RUAccent's own accents.json/omographs.json: genuine unstressed
  # skips (names/rare words) + stress-homographs. Skipped if the dicts aren't present.
  DICT=${DICT:-$F5_HOME/ru-dict}
  [ -f "$DICT/accents.json" ] && python3 "$F5_HOME/dict_flags.py" \
    "$DICT/accents.json" "$DICT/omographs.json" "$REVIEW" || true
  "$F5PY" - "$REVIEW" "$REVIEW/_homographs.txt" > "$REVIEW/_check-yo.tsv" <<'PY'
import glob, os, sys
rev, homf = sys.argv[1], sys.argv[2]
strip = lambda w: w.replace("+", "")
base = lambda w: strip(w).lower().strip('.,!?;:»«"()—-')
homs = set(l.strip() for l in open(homf, encoding="utf-8")) if os.path.exists(homf) else set()
print("part\tchunk\tword\t(ambiguous ё-homograph — verify noun/verb/case in the .txt)")
for txt in sorted(glob.glob(os.path.join(rev, "*[0-9]_*.txt"))):
    if txt.endswith(".raw.txt"): continue
    raw = txt[:-4] + ".raw.txt"
    if not os.path.exists(raw): continue
    part = os.path.basename(txt).split("_")[0]
    for i, (a, b) in enumerate(zip(open(raw, encoding="utf-8"), open(txt, encoding="utf-8")), 1):
        aw, bw = a.split(), b.split()
        if len(aw) != len(bw): continue
        for x, y in zip(aw, bw):
            if "ё" in strip(y).lower() and "ё" not in x.lower() and base(x) in homs:
                print(f"{part}\t{i}\t{x}→{strip(y)}")
PY
  echo "→ review files in $REVIEW/  (NN_*.txt = editable stressed text; _check-yo.tsv = ё-flags)"
  [ "$MODE" = stress ] && { echo "✓ stress done — edit the .txt files, then run MODE=synth"; exit 0; }
fi

# ---- SYNTH phase: read (edited) per-part stressed text -> F5 -> mp3 --------------------------
WORK=$(mktemp -d); trap 'rm -rf "$WORK"' EXIT
: > "$WORK/reqs.ndjson"
gidx=0
while IFS=$'\t' read -r pp safe title; do
  f="$REVIEW/${pp}_${safe}.txt"; [ -f "$f" ] || continue
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    gidx=$((gidx+1))
    out=$(printf '%s/p%s_c%05d.wav' "$WORK" "$pp" "$gidx")
    python3 -c 'import json,sys;print(json.dumps({"text":sys.argv[1],"output":sys.argv[2]},ensure_ascii=False))' "$line" "$out"
  done < "$f"
done < "$REVIEW/_titles.tsv" > "$WORK/reqs.ndjson"
N=$(wc -l < "$WORK/reqs.ndjson" | tr -d ' ')
[ "$N" -gt 0 ] || { echo "no stressed text — run MODE=stress first"; exit 1; }
if [ "$ENGINE" = native ]; then
  [ -x "$F5BIN" ] || { echo "native engine: F5BIN not built ($F5BIN) — cargo build --release in sopds-tts-rs"; exit 1; }
  [ -f "$F5MODEL/F5_Transformer.onnx" ] || { echo "native engine: F5MODEL not a model dir ($F5MODEL)"; exit 1; }
  echo "→ synthesizing $N chunks on $WORKERS native daemon(s) — $F5MODEL (nfe=32 baked)"
else
  echo "→ synthesizing $N chunks on $WORKERS python daemon(s) (nfe=$NFE $DEVICE)"
fi

SECONDS=0; pids=()
if [ "$ENGINE" = native ]; then
  # Route footnote chunks (each begins "Примечание.", RUAccent → "Примеч+ание.") to the 2nd voice;
  # everything else to the main voice. Both write p*_c*.wav by global index, so the join is unaffected.
  grep -E '"text": *"Примеч[+]?ание\.' "$WORK/reqs.ndjson" > "$WORK/notes.ndjson" || true
  grep -vE '"text": *"Примеч[+]?ание\.' "$WORK/reqs.ndjson" > "$WORK/narr.ndjson" || true
  for ((i=0;i<WORKERS;i++)); do
    awk -v W="$WORKERS" -v id="$i" 'NR%W==id' "$WORK/narr.ndjson" > "$WORK/shard_$i"
    # Rust/ort daemon: same NDJSON {text,output}. THREADS caps ORT intra-op so WORKERS>1 don't oversubscribe.
    SOPDS_TTS_THREADS="${THREADS:-0}" "$F5BIN" "$F5MODEL" \
       < "$WORK/shard_$i" > "$WORK/resp_$i" 2>"$WORK/dlog_$i" &
    pids+=($!)
  done
  if [ -s "$WORK/notes.ndjson" ]; then
    if [ -f "$NOTES_VOICES/manifest.txt" ]; then
      # CAST MODE: each footnote → a different voice, rotated in book order. Voices run one at a time
      # in a bg subshell (bounded memory: 1 note-daemon + WORKERS narrative daemons); join by index.
      VOICES=(); while IFS= read -r v; do [ -n "$v" ] && VOICES+=("$v"); done < "$NOTES_VOICES/manifest.txt"
      K=${#VOICES[@]}
      i=0
      while IFS= read -r line; do
        printf '%s\n' "$line" >> "$WORK/notes_${VOICES[$((i % K))]}.ndjson"; i=$((i+1))
      done < "$WORK/notes.ndjson"
      echo "  ($i footnote chunks → $K voices: ${VOICES[*]})"
      (
        for v in "${VOICES[@]}"; do
          [ -s "$WORK/notes_$v.ndjson" ] || continue
          SOPDS_TTS_THREADS="${THREADS:-0}" "$F5BIN" "$NOTES_VOICES/$v" \
            < "$WORK/notes_$v.ndjson" >> "$WORK/resp_notes" 2>>"$WORK/dlog_notes"
        done
      ) &
      pids+=($!)
    else
      [ -f "$F5MODEL_NOTES/F5_Transformer.onnx" ] || { echo "footnote voice: F5MODEL_NOTES not a model dir ($F5MODEL_NOTES)"; exit 1; }
      echo "  ($(wc -l < "$WORK/notes.ndjson"|tr -d ' ') footnote chunks → $F5MODEL_NOTES)"
      SOPDS_TTS_THREADS="${THREADS:-0}" "$F5BIN" "$F5MODEL_NOTES" \
         < "$WORK/notes.ndjson" > "$WORK/resp_notes" 2>>"$WORK/dlog_notes" &
      pids+=($!)
    fi
  fi
else
  for ((i=0;i<WORKERS;i++)); do
    awk -v W="$WORKERS" -v id="$i" 'NR%W==id' "$WORK/reqs.ndjson" > "$WORK/shard_$i"
    "$F5PY" "$F5_HOME/f5_daemon.py" --ckpt "$CKPT" --vocab "$VOCAB" --ref "$REF" \
       --ref-text "$REF_TEXT" --nfe "$NFE" --device "$DEVICE" ${REMOVE_SILENCE:+--remove-silence} \
       < "$WORK/shard_$i" > "$WORK/resp_$i" 2>"$WORK/dlog_$i" &
    pids+=($!)
  done
fi
while :; do
  alive=0; for pd in "${pids[@]}"; do kill -0 "$pd" 2>/dev/null && { alive=1; break; }; done
  done=$(find "$WORK" -maxdepth 1 -name 'p*_c*.wav' | wc -l | tr -d ' ')
  printf '\r\033[K  %d/%d  %dm%02ds' "$done" "$N" $((SECONDS/60)) $((SECONDS%60))
  [ "$alive" -eq 0 ] && break; sleep 3
done
wait "${pids[@]}" 2>/dev/null || true
printf '\r\033[K  %d/%d done in %dm%02ds\n' "$(find "$WORK" -maxdepth 1 -name 'p*_c*.wav'|wc -l|tr -d ' ')" "$N" $((SECONDS/60)) $((SECONDS%60))

# A footnote is read in a different voice; bracket it so the listener hears the aside start/end:
# a gentle airport-style "пім-пуум" chime BEFORE (NOTE_CHIME wav, or a built-in A5→E5 bell), and
# NOTE_PAUSE (default 0.3s) of silence AFTER.
NOTE_PAUSE=${NOTE_PAUSE:-0.3}
NOTE_CHIME=${NOTE_CHIME:-}   # path to a chime wav; empty ⇒ generate the built-in bell
if [ -f "$WORK/notes.ndjson" ]; then
  awk "BEGIN{exit !($NOTE_PAUSE>0)}" && \
    ffmpeg -y -hide_banner -loglevel error -f lavfi -i anullsrc=r=24000:cl=mono -t "$NOTE_PAUSE" -c:a pcm_s16le "$WORK/pause.wav"
  if [ -n "$NOTE_CHIME" ] && [ -f "$NOTE_CHIME" ]; then
    ffmpeg -y -hide_banner -loglevel error -i "$NOTE_CHIME" -ar 24000 -ac 1 -c:a pcm_s16le "$WORK/chime.wav"
  else   # built-in soft two-tone bell: A5(880) short + E5(659) longer, exp decay, lowpass+echo
    ffmpeg -y -hide_banner -loglevel error -f lavfi -i "sine=frequency=880:duration=0.6" -af "volume='exp(-7*t)':eval=frame,afade=t=in:d=0.008" -ar 24000 -ac 1 "$WORK/ch1.wav"
    ffmpeg -y -hide_banner -loglevel error -f lavfi -i "sine=frequency=659:duration=1.1" -af "volume='exp(-3.5*t)':eval=frame,afade=t=in:d=0.008" -ar 24000 -ac 1 "$WORK/ch2.wav"
    printf "file '%s/ch1.wav'\nfile '%s/ch2.wav'\n" "$WORK" "$WORK" > "$WORK/chl.txt"
    ffmpeg -y -hide_banner -loglevel error -f concat -safe 0 -i "$WORK/chl.txt" -af "lowpass=f=6500,aecho=0.8:0.6:60:0.25,volume=0.5" -ar 24000 -ac 1 -c:a pcm_s16le "$WORK/chime.wav"
  fi
  grep -ho '"output": *"[^"]*"' "$WORK/notes.ndjson" | sed 's/.*"output": *"//; s/"$//' | while read -r p; do basename "$p"; done > "$WORK/note_wavs.txt"
fi

echo "→ joining parts…"
while IFS=$'\t' read -r pp safe title; do
  files=$(find "$WORK" -maxdepth 1 -name "p${pp}_c*.wav" | sort); [ -n "$files" ] || continue
  : > "$WORK/list_$pp.txt"
  while IFS= read -r wf; do
    [ -n "$wf" ] || continue
    if [ -f "$WORK/note_wavs.txt" ] && grep -qxF "$(basename "$wf")" "$WORK/note_wavs.txt" 2>/dev/null; then
      [ -f "$WORK/chime.wav" ] && printf "file '%s'\n" "$WORK/chime.wav" >> "$WORK/list_$pp.txt"  # пім-пуум before
      printf "file '%s'\n" "$wf" >> "$WORK/list_$pp.txt"
      [ -f "$WORK/pause.wav" ] && printf "file '%s'\n" "$WORK/pause.wav" >> "$WORK/list_$pp.txt"  # pause after
    else
      printf "file '%s'\n" "$wf" >> "$WORK/list_$pp.txt"
    fi
  done <<< "$files"
  o="$OUT/${pp}_${safe}.mp3"
  ffmpeg -y -hide_banner -loglevel error -f concat -safe 0 -i "$WORK/list_$pp.txt" -c:a libmp3lame -b:a 64k -ac 1 "$o"
  echo "  ✓ $o ($(du -h "$o"|cut -f1))"
done < "$REVIEW/_titles.tsv"
echo "✓ done → $OUT"
