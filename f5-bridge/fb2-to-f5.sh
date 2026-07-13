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
for ((i=0;i<WORKERS;i++)); do
  awk -v W="$WORKERS" -v id="$i" 'NR%W==id' "$WORK/reqs.ndjson" > "$WORK/shard_$i"
  if [ "$ENGINE" = native ]; then
    # Rust/ort daemon: same NDJSON {text,output}. Cap ORT intra-op threads so WORKERS>1 don't
    # oversubscribe; 0/unset = all cores (fine for WORKERS=1). ref/vocab/graphs live in F5MODEL.
    SOPDS_TTS_THREADS="${THREADS:-0}" "$F5BIN" "$F5MODEL" \
       < "$WORK/shard_$i" > "$WORK/resp_$i" 2>"$WORK/dlog_$i" &
  else
    "$F5PY" "$F5_HOME/f5_daemon.py" --ckpt "$CKPT" --vocab "$VOCAB" --ref "$REF" \
       --ref-text "$REF_TEXT" --nfe "$NFE" --device "$DEVICE" ${REMOVE_SILENCE:+--remove-silence} \
       < "$WORK/shard_$i" > "$WORK/resp_$i" 2>"$WORK/dlog_$i" &
  fi
  pids+=($!)
done
while :; do
  alive=0; for pd in "${pids[@]}"; do kill -0 "$pd" 2>/dev/null && { alive=1; break; }; done
  done=$(find "$WORK" -maxdepth 1 -name 'p*_c*.wav' | wc -l | tr -d ' ')
  printf '\r\033[K  %d/%d  %dm%02ds' "$done" "$N" $((SECONDS/60)) $((SECONDS%60))
  [ "$alive" -eq 0 ] && break; sleep 3
done
wait "${pids[@]}" 2>/dev/null || true
printf '\r\033[K  %d/%d done in %dm%02ds\n' "$(find "$WORK" -maxdepth 1 -name 'p*_c*.wav'|wc -l|tr -d ' ')" "$N" $((SECONDS/60)) $((SECONDS%60))

echo "→ joining parts…"
while IFS=$'\t' read -r pp safe title; do
  files=$(find "$WORK" -maxdepth 1 -name "p${pp}_c*.wav" | sort); [ -n "$files" ] || continue
  echo "$files" | sed "s|^|file '|; s|$|'|" > "$WORK/list_$pp.txt"
  o="$OUT/${pp}_${safe}.mp3"
  ffmpeg -y -hide_banner -loglevel error -f concat -safe 0 -i "$WORK/list_$pp.txt" -c:a libmp3lame -b:a 64k -ac 1 "$o"
  echo "  ✓ $o ($(du -h "$o"|cut -f1))"
done < "$REVIEW/_titles.tsv"
echo "✓ done → $OUT"
