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
# env: NFE=32 WORKERS=1 MAXCHARS=250 DEVICE=cpu REMOVE_SILENCE=1  FIX=corrections.json  (NFE 16 = muffled)
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
NFE=${NFE:-32}; WORKERS=${WORKERS:-1}; MAXCHARS=${MAXCHARS:-250}; DEVICE=${DEVICE:-cpu}
MODE=${MODE:-all}; PARTS=${PARTS:-}; FIX=${FIX:-}
# Accent mode per language (STRESS_MODE, from the worker's lc.stress):
#   ruaccent (ru, default) — native Rust `sopds-tts-rs stress` (needs RUACCENT_HOME + STRESSBIN/F5BIN);
#   none     (en)          — F5 English reads plain text, so the extracted text is used as-is;
#   uk-stress (uk)         — a Ukrainian stresser (TBD).
STRESS_MODE="${STRESS_MODE:-ruaccent}"
if [ "$STRESS_MODE" = ruaccent ]; then
  export RUACCENT_HOME="${RUACCENT_HOME:-$HOME/.cache/ruaccent}"
  _stressbin="${STRESSBIN:-${F5BIN:-}}"
  [ -n "$_stressbin" ] || { echo "fb2-to-f5.sh: set STRESSBIN or F5BIN to the sopds-tts-rs binary" >&2; exit 1; }
  STRESS=("$_stressbin" stress)
fi
REVIEW="$OUT/review"
mkdir -p "$OUT" "$REVIEW"
# SOPDS — the sopds binary providing the native `fb2-extract` (FB2 → per-unit narration text: 2-level
# section OR flat bold-heading split, PARTS selector, COMBINE granularity, spoken headings, inlined
# Примечания). The worker passes its own executable path; fall back to `sopds` on PATH for manual runs.
SOPDS="${SOPDS:-sopds}"

# ---- STRESS phase: extract narration (native) → stress each unit -----------------------------
if [ "$MODE" = stress ] || [ "$MODE" = all ]; then
  echo "→ extracting narration (native fb2-extract; PARTS='${PARTS:-all}' COMBINE=${COMBINE:-1})…"
  "$SOPDS" fb2-extract "$FB2" "$REVIEW" "$MAXCHARS" "${PARTS:-}" --combine "${COMBINE:-1}" \
    ${NOTE_PREFIX:+--note-prefix "$NOTE_PREFIX"}
  N=$(wc -l < "$REVIEW/_titles.tsv" | tr -d ' ')
  echo "→ stressing $N unit(s) (chars≤$MAXCHARS)"
  while IFS=$'\t' read -r id safe title; do
    raw="$REVIEW/${id}_${safe}.raw.txt"; out="$REVIEW/${id}_${safe}.txt"
    case "$STRESS_MODE" in
      none)     cp "$raw" "$out" ;;                                                            # English: verbatim
      ruaccent) "${STRESS[@]}" ${FIX:+--fix "$FIX"} < "$raw" > "$out" 2>>"$REVIEW/_ruaccent.log" ;;
      *)        echo "fb2-to-f5.sh: unknown STRESS_MODE='$STRESS_MODE'" >&2; exit 1 ;;
    esac
    echo "  ✓ ${id}: $(wc -l < "$out") chunks — $title"
  done < "$REVIEW/_titles.tsv"
  # ё-homograph review report — RUAccent only (English/none has no ё).
  if [ "$STRESS_MODE" = ruaccent ]; then
    "${STRESS[@]}" --dump-homographs "$REVIEW/_homographs.txt" </dev/null 2>/dev/null || true
    "$SOPDS" check-yo "$REVIEW" "$REVIEW/_homographs.txt" > "$REVIEW/_check-yo.tsv" 2>/dev/null || true
  fi
  echo "→ review files in $REVIEW/  (NN_*.txt = editable stressed text; _check-yo.tsv = ё-flags)"
  [ "$MODE" = stress ] && { echo "✓ stress done — edit the .txt files, then run MODE=synth"; exit 0; }
fi

# ---- SYNTH phase: read (edited) per-part stressed text -> F5 -> mp3 --------------------------
WORK=$(mktemp -d); trap 'rm -rf "$WORK"' EXIT
# Build the daemon's request stream natively: one {"text","output"} per stressed chunk. Dual-voice: when
# F5MODEL_NOTES is set, footnote chunks (flagged by the .notes sidecar) go to reqs_notes.ndjson for a
# second voice; otherwise everything is one stream read by the main voice.
NOTES_REQS="$WORK/reqs_notes.ndjson"; : > "$NOTES_REQS"
if [ -n "${F5MODEL_NOTES:-}" ]; then
  "$SOPDS" ndjson-reqs "$REVIEW" "$WORK" --notes-file "$NOTES_REQS" > "$WORK/reqs.ndjson"
else
  "$SOPDS" ndjson-reqs "$REVIEW" "$WORK" > "$WORK/reqs.ndjson"
fi
NMAIN=$(wc -l < "$WORK/reqs.ndjson" | tr -d ' ')
NNOTES=$(wc -l < "$NOTES_REQS" | tr -d ' ')
N=$((NMAIN + NNOTES))
[ "$N" -gt 0 ] || { echo "no stressed text — run MODE=stress first"; exit 1; }
# Engine: native Rust (ort) F5. The model DIR carries ckpt/vocab/ref; the daemon speaks the NDJSON
# {"text","output"} protocol and reads NFE from SOPDS_TTS_NFE ($NFE, default 32 — 16 sounds muffled).
[ -n "${F5BIN:-}" ] || { echo "fb2-to-f5.sh: set F5BIN to the sopds-tts-rs binary (native F5 synth)" >&2; exit 1; }
export SOPDS_TTS_NFE="$NFE"
[ "$NNOTES" -gt 0 ] && echo "→ synthesizing $NMAIN narration + $NNOTES footnote chunks (native rust nfe=$NFE; notes voice: $F5MODEL_NOTES)" \
                    || echo "→ synthesizing $N chunks on $WORKERS daemon(s) (native rust nfe=$NFE)"

# Footnote chimes: the warm two-tone airport bell (the sound from the old Python pipeline — a real decaying
# bell, not flat sine tones) — falling A5→E5 "пім-пуум" INTO a note run, rising E5→A5 back to narration.
# Prefer the saved/tweakable files under $F5_HOME/chimes (deploy with the voice assets — drop a real airport
# sample there to override); regenerate the built-in bell into $WORK only if absent. Override with
# CHIME_IN/CHIME_OUT; CHIME=0 disables. Only when there are note chunks (a notes voice is in play).
CHIME=${CHIME:-1}
if [ "$NNOTES" -gt 0 ] && [ "$CHIME" != 0 ]; then
  _mkbell() { # <freq_short> <freq_long> <out> — two-tone decaying bell (exp decay, lowpass + light echo)
    ffmpeg -y -v error -f lavfi -i "sine=frequency=$1:duration=0.6" -af "volume='exp(-7*t)':eval=frame,afade=t=in:d=0.008"   -ar 24000 -ac 1 "$WORK/_ch1.wav"
    ffmpeg -y -v error -f lavfi -i "sine=frequency=$2:duration=1.1" -af "volume='exp(-3.5*t)':eval=frame,afade=t=in:d=0.008" -ar 24000 -ac 1 "$WORK/_ch2.wav"
    printf "file '%s/_ch1.wav'\nfile '%s/_ch2.wav'\n" "$WORK" "$WORK" > "$WORK/_chl.txt"
    ffmpeg -y -v error -f concat -safe 0 -i "$WORK/_chl.txt" -af "lowpass=f=6500,aecho=0.8:0.6:60:0.25,volume=0.5" -ar 24000 -ac 1 -c:a pcm_s16le "$3"
  }
  CHIME_IN=${CHIME_IN:-$F5_HOME/chimes/chime_in.wav};   [ -f "$CHIME_IN" ]  || { CHIME_IN=$WORK/chime_in.wav;   _mkbell 880 659 "$CHIME_IN"; }   # A5→E5 falling → into note
  CHIME_OUT=${CHIME_OUT:-$F5_HOME/chimes/chime_out.wav}; [ -f "$CHIME_OUT" ] || { CHIME_OUT=$WORK/chime_out.wav; _mkbell 659 880 "$CHIME_OUT"; }  # E5→A5 rising  → back to narration
else
  CHIME_IN=""; CHIME_OUT=""
fi

SECONDS=0; pids=()
for ((i=0;i<WORKERS;i++)); do
  gawk -v W="$WORKERS" -v id="$i" 'NR%W==id' "$WORK/reqs.ndjson" > "$WORK/shard_$i"
  "$F5BIN" "$F5MODEL" < "$WORK/shard_$i" > "$WORK/resp_$i" 2>"$WORK/dlog_$i" &
  pids+=($!)
done
# Footnote chunks → the notes voice (one extra daemon; footnotes are few). Writes p*_c*.wav into $WORK
# just like the main daemons, so the join below is voice-agnostic.
if [ "$NNOTES" -gt 0 ]; then
  "$F5BIN" "$F5MODEL_NOTES" < "$NOTES_REQS" > "$WORK/resp_notes" 2>"$WORK/dlog_notes" &
  pids+=($!)
fi
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
  list="$WORK/list_$pp.txt"; : > "$list"
  # Concat in c-number order, bracketing each contiguous run of .note.wav (one footnote) with chimes.
  prev_note=0
  while IFS= read -r wf; do
    [ -n "$wf" ] || continue
    case "$wf" in *.note.wav) is_note=1;; *) is_note=0;; esac
    if [ -n "$CHIME_IN" ] && [ "$is_note" = 1 ] && [ "$prev_note" = 0 ]; then printf "file '%s'\n" "$CHIME_IN"  >> "$list"; fi
    if [ -n "$CHIME_IN" ] && [ "$is_note" = 0 ] && [ "$prev_note" = 1 ]; then printf "file '%s'\n" "$CHIME_OUT" >> "$list"; fi
    printf "file '%s'\n" "$wf" >> "$list"
    prev_note=$is_note
  done <<< "$files"
  if [ -n "$CHIME_IN" ] && [ "$prev_note" = 1 ]; then printf "file '%s'\n" "$CHIME_OUT" >> "$list"; fi  # part ends inside a note
  o="$OUT/${pp}_${safe}.mp3"
  ffmpeg -y -hide_banner -loglevel error -f concat -safe 0 -i "$list" -c:a libmp3lame -b:a 64k -ac 1 "$o"
  echo "  ✓ $o ($(du -h "$o"|cut -f1))"
done < "$REVIEW/_titles.tsv"
echo "✓ done → $OUT"
