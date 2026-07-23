#!/usr/bin/env bash
# fb2-to-f5.sh — narrate an FB2 book with F5-TTS in a cloned voice, one MP3 per part.
#
#   fb2-to-f5.sh <book.fb2> [out_dir]
#
# Three modes (env MODE):
#   MODE=stress  extract -> chunk -> RUAccent -> write reviewable out/review/NN_<title>.txt
#                (one stressed chunk per line) + out/review/_check-yo.tsv (ё-additions to eyeball).
#                Edit those .txt files to fix any stress, then run MODE=synth.
#   MODE=synth   read out/review/NN_*.txt (as edited) -> F5 daemons -> out/NN_<title>.mp3
#   MODE=all     (default) stress then synth in one pass, no review stop.
#
# env: NFE=16 WORKERS=1 MAXCHARS=250 DEVICE=cpu REMOVE_SILENCE=1  FIX=corrections.json
#      REF/REF_TEXT/CKPT/VOCAB/F5_HOME
#      PARTS — which sections to narrate (empty ⇒ all top-level sections). Two levels of hierarchy:
#         "P"        whole top-level section P            (e.g. PARTS="3")
#         "P1-P2"    a range of whole top sections        (e.g. PARTS="1-3")
#         "P:S"      nested section S inside part P        (e.g. PARTS="1:2")
#         "P:S1-S2"  nested sections S1..S2 inside part P  (e.g. PARTS="4:2-15")
#       space-separate to combine: PARTS="1:2 2:1-3 4". S is the POSITION within P (not a global
#       chapter number). Output/MP3 is one file per unit (NN or NN.MM). The stress phase prints the
#       book's section map so you can see the boundaries.
#      COMBINE — MP3 granularity for a whole-part selection: 1 (default) = one MP3 per top-level
#       section; 2 = one MP3 per nested section. (Explicit "P:S" is always per nested section.)
#
# BRIDGE ONLY — native Rust (ort) F5 replaces this Python; see docs/decisions/001.
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
# Stress engine: native Rust `sopds-tts-rs stress` (STRESSBIN, else the F5BIN binary — it's the same
# binary). Needs RUACCENT_HOME (the dictionary + nn models dir). Accepts --fix / --dump-homographs.
export RUACCENT_HOME="${RUACCENT_HOME:-$HOME/.cache/ruaccent}"
_stressbin="${STRESSBIN:-${F5BIN:-}}"
[ -n "$_stressbin" ] || { echo "fb2-to-f5.sh: set STRESSBIN or F5BIN to the sopds-tts-rs binary" >&2; exit 1; }
STRESS=("$_stressbin" stress)
REVIEW="$OUT/review"
mkdir -p "$OUT" "$REVIEW"
# SOPDS — the sopds binary providing the native `fb2-extract` (FB2 → per-unit narration text: 2-level
# section OR flat bold-heading split, PARTS selector, COMBINE granularity, spoken headings, inlined
# Примечания). The worker passes its own executable path; fall back to `sopds` on PATH for manual runs.
SOPDS="${SOPDS:-sopds}"

# ---- STRESS phase: extract narration (native) → stress each unit -----------------------------
if [ "$MODE" = stress ] || [ "$MODE" = all ]; then
  echo "→ extracting narration (native fb2-extract; PARTS='${PARTS:-all}' COMBINE=${COMBINE:-1})…"
  "$SOPDS" fb2-extract "$FB2" "$REVIEW" "$MAXCHARS" "${PARTS:-}" --combine "${COMBINE:-1}"
  N=$(wc -l < "$REVIEW/_titles.tsv" | tr -d ' ')
  echo "→ stressing $N unit(s) (chars≤$MAXCHARS)"
  while IFS=$'\t' read -r id safe title; do
    "${STRESS[@]}" ${FIX:+--fix "$FIX"} \
      < "$REVIEW/${id}_${safe}.raw.txt" \
      > "$REVIEW/${id}_${safe}.txt" 2>>"$REVIEW/_ruaccent.log"
    echo "  ✓ ${id}: $(wc -l < "$REVIEW/${id}_${safe}.txt") chunks — $title"
  done < "$REVIEW/_titles.tsv"
  # Ambiguous-homograph report: only flag ё-restorations on genuine homographs (берет, десны, …),
  # not the always-ё words (ещё, всё, её). These are the ones worth eyeballing in the review text.
  "${STRESS[@]}" --dump-homographs "$REVIEW/_homographs.txt" </dev/null 2>/dev/null || true
  "$SOPDS" check-yo "$REVIEW" "$REVIEW/_homographs.txt" > "$REVIEW/_check-yo.tsv" 2>/dev/null || true
  echo "→ review files in $REVIEW/  (NN_*.txt = editable stressed text; _check-yo.tsv = ё-flags)"
  [ "$MODE" = stress ] && { echo "✓ stress done — edit the .txt files, then run MODE=synth"; exit 0; }
fi

# ---- SYNTH phase: read (edited) per-part stressed text -> F5 -> mp3 --------------------------
WORK=$(mktemp -d); trap 'rm -rf "$WORK"' EXIT
# Build the daemon's request stream natively: one {"text","output"} per stressed chunk.
"$SOPDS" ndjson-reqs "$REVIEW" "$WORK" > "$WORK/reqs.ndjson"
N=$(wc -l < "$WORK/reqs.ndjson" | tr -d ' ')
[ "$N" -gt 0 ] || { echo "no stressed text — run MODE=stress first"; exit 1; }
# Engine: native Rust (ort) F5. The model DIR carries ckpt/vocab/ref; the daemon speaks the NDJSON
# {"text","output"} protocol and reads NFE from SOPDS_TTS_NFE ($NFE, default 16 — ~2x faster than 32).
[ -n "${F5BIN:-}" ] || { echo "fb2-to-f5.sh: set F5BIN to the sopds-tts-rs binary (native F5 synth)" >&2; exit 1; }
export SOPDS_TTS_NFE="$NFE"
echo "→ synthesizing $N chunks on $WORKERS daemon(s) (native rust nfe=$NFE)"

SECONDS=0; pids=()
for ((i=0;i<WORKERS;i++)); do
  gawk -v W="$WORKERS" -v id="$i" 'NR%W==id' "$WORK/reqs.ndjson" > "$WORK/shard_$i"
  "$F5BIN" "$F5MODEL" < "$WORK/shard_$i" > "$WORK/resp_$i" 2>"$WORK/dlog_$i" &
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
