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
# Pauses the joiner inserts, because the model cannot. Ending a chunk on its ?/!/… is what buys the
# phrase contour, but it also takes the pause away from F5: a sentence-final ellipsis gets ~530 ms
# against 720 for a full stop, and a chunk boundary gets nothing at all — with TRIM_SILENCE the tails
# are trimmed too. So every chunk gets a deterministic pause AFTER it, sized by its own trailing mark.
# This is what lets the chunker split on commas freely: the pause length, not the split point, sets
# the prosody. PUNCT_PAUSE=0 falls back to one flat SENT_PAUSE at sentence ends only.
PUNCT_PAUSE=${PUNCT_PAUSE:-1}
PAUSE_DOT=${PAUSE_DOT:-0.5}   PAUSE_COMMA=${PAUSE_COMMA:-0.25} PAUSE_DASH=${PAUSE_DASH:-0.7}
PAUSE_ELLIPSIS=${PAUSE_ELLIPSIS:-0.8} PAUSE_SEMI=${PAUSE_SEMI:-0.4} PAUSE_COLON=${PAUSE_COLON:-0.3}
PAUSE_DEFAULT=${PAUSE_DEFAULT:-0.15} SENT_PAUSE=${SENT_PAUSE:-0.30}
# TITLE_PAUSE brackets each heading chunk on BOTH sides — the punctuation map cannot express that,
# because it only ever places a pause after a chunk. K per part comes from review/_titlecount.tsv.
TITLE_PAUSE=${TITLE_PAUSE:-1.5}
# NOTE_PAUSE follows a footnote, after its closing chime. PUNCT_CLEAN rewrites a trailing '…' to '.'
# and drops a trailing spaced em dash before synthesis — F5 renders both poorly, and the pause they
# were carrying is inserted at join time anyway.
NOTE_PAUSE=${NOTE_PAUSE:-0.3}; PUNCT_CLEAN=${PUNCT_CLEAN:-1}
# TRIM_SILENCE strips each chunk's own leading/trailing silence so the deterministic pause above is
# the only gap at a seam. It costs one ffmpeg per chunk (~0.5 s), which is the bulk of the join time.
TRIM_SILENCE=${TRIM_SILENCE:-1}
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
elif [ "$STRESS_MODE" = uk-stress ]; then
  # native Go uk-stress: VESUM dict lookup, "+"-marks; unknown/homograph words left
  # unstressed for the reviewer to fix. Build: (cd f5-bridge/uk-stress && go build -o uk-stress .)
  _ukbin="${UKSTRESSBIN:-uk-stress}"
  _ukdict="${UKDICT:-$HOME/src/f5-spike/uk-data/uk_stress.tsv}"
  _ukhomo="${UKHOMO:-$HOME/src/f5-spike/uk-data/uk_stress_homographs.txt}"
  _ukdef="${UKDEFAULTS:-$HOME/src/f5-spike/uk-data/uk_stress_defaults.tsv}"   # real-usage default for homographs
  [ -s "$_ukdict" ] || { echo "fb2-to-f5.sh: uk-stress dict not found ($_ukdict); set UKDICT" >&2; exit 1; }
  STRESS=("$_ukbin" -dict "$_ukdict" -homographs "$_ukhomo" ${_ukdef:+-defaults "$_ukdef"})
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
      none)      cp "$raw" "$out" ;;                                                           # English: verbatim
      ruaccent)  "${STRESS[@]}" ${FIX:+--fix "$FIX"} < "$raw" > "$out" 2>>"$REVIEW/_ruaccent.log" ;;
      uk-stress) "${STRESS[@]}" < "$raw" > "$out" 2>>"$REVIEW/_ukstress.log" ;;               # Ukrainian: VESUM dict
      *)         echo "fb2-to-f5.sh: unknown STRESS_MODE='$STRESS_MODE'" >&2; exit 1 ;;
    esac
    echo "  ✓ ${id}: $(wc -l < "$out") chunks — $title"
  done < "$REVIEW/_titles.tsv"
  # ё-homograph review report — RUAccent only (English/none has no ё).
  if [ "$STRESS_MODE" = ruaccent ]; then
    "${STRESS[@]}" --dump-homographs "$REVIEW/_homographs.txt" </dev/null 2>/dev/null || true
    "$SOPDS" check-yo "$REVIEW" "$REVIEW/_homographs.txt" > "$REVIEW/_check-yo.tsv" 2>/dev/null || true
  fi
  # UK: sidecar the reviewer reads as OMO (stress-homograph bases). It then colours a
  # STRESSED homograph as "verify me" (a default was applied — glance & confirm) and an
  # UNSTRESSED one as "mark me" — exactly the two states we want reviewed.
  if [ "$STRESS_MODE" = uk-stress ]; then
    cut -f1 "$_ukhomo" > "$REVIEW/_omographs.txt" 2>/dev/null || true
  fi
  echo "→ review files in $REVIEW/  (NN_*.txt = editable stressed text; _check-yo.tsv = ё-flags)"
  [ "$MODE" = stress ] && { echo "✓ stress done — edit the .txt files, then run MODE=synth"; exit 0; }
fi

# ---- SYNTH phase: read (edited) per-part stressed text -> F5 -> mp3 --------------------------
# WORK_DIR keeps the per-chunk wavs after the run. Pause lengths are a JOIN-time parameter, so with
# the wavs on disk a change of any PAUSE_* is a two-minute re-join instead of a full re-synthesis.
# Unset ⇒ a temp dir, removed on exit. JOIN_ONLY=1 re-joins from an existing WORK_DIR.
if [ -n "${WORK_DIR:-}" ]; then WORK="$WORK_DIR"; mkdir -p "$WORK"
else WORK=$(mktemp -d); trap 'rm -rf "$WORK"' EXIT; fi
JOIN_ONLY=${JOIN_ONLY:-0}

# Build the daemon's request stream natively: one {"text","output"} per stressed chunk. Dual-voice:
# with F5MODEL_NOTES (one voice) or NOTES_VOICES (a cast, manifest.txt = one dir per voice), footnote
# chunks go to a separate stream; otherwise everything is read by the main voice.
NOTES_REQS="$WORK/reqs_notes.ndjson"; : > "$NOTES_REQS"
NOTES_ON=0
if [ -n "${F5MODEL_NOTES:-}" ]; then NOTES_ON=1; fi
if [ -n "${NOTES_VOICES:-}" ] && [ -f "${NOTES_VOICES}/manifest.txt" ]; then NOTES_ON=1; fi
if [ "$NOTES_ON" = 1 ]; then
  "$SOPDS" ndjson-reqs "$REVIEW" "$WORK" --notes-file "$NOTES_REQS" > "$WORK/reqs.ndjson"
else
  "$SOPDS" ndjson-reqs "$REVIEW" "$WORK" > "$WORK/reqs.ndjson"
fi

# Footnotes without a sidecar. `ndjson-reqs` finds notes from the `<id>_<safe>.notes` mask, which only
# `sopds fb2-extract` writes — a review dir proofread before that change has no mask, and its notes
# would silently be read by the narrator, mid-sentence, with the word "Примечание" spoken aloud. So
# fall back to the marker the text itself carries. Warn rather than do it quietly: if the prefix is
# wrong for this book, nothing else will say so.
NOTE_PREFIX_RE=${NOTE_PREFIX:-}
if [ -z "$NOTE_PREFIX_RE" ] && [ "$STRESS_MODE" = ruaccent ]; then NOTE_PREFIX_RE='Примеч[+]?ание\.'; fi
if [ "$NOTES_ON" = 1 ] && [ ! -s "$NOTES_REQS" ] && [ -n "$NOTE_PREFIX_RE" ]; then
  grep -E "\"text\": *\"$NOTE_PREFIX_RE" "$WORK/reqs.ndjson" > "$NOTES_REQS" || true
  if [ -s "$NOTES_REQS" ]; then
    grep -vE "\"text\": *\"$NOTE_PREFIX_RE" "$WORK/reqs.ndjson" > "$WORK/reqs.narr" || true
    mv "$WORK/reqs.narr" "$WORK/reqs.ndjson"
    echo "  (no .notes sidecar — $(wc -l < "$NOTES_REQS" | tr -d ' ') footnotes found by prefix instead)"
  fi
fi
NMAIN=$(wc -l < "$WORK/reqs.ndjson" | tr -d ' ')
NNOTES=$(wc -l < "$NOTES_REQS" | tr -d ' ')
N=$((NMAIN + NNOTES))
[ "$N" -gt 0 ] || { echo "no stressed text — run MODE=stress first"; exit 1; }

# One plan for the whole join: per chunk, how long a pause follows it and what kind of chunk it is.
# Derived from the request stream itself, because that is the only thing that knows which wav a chunk
# became — nothing here re-guesses the numbering. ndjson-reqs encodes a Go map, so the keys come out
# alphabetically: {"output":…,"text":…}, i.e. the text is last and ends the line with "}.
# The same pass applies PUNCT_CLEAN, and it reads the ORIGINAL trailing mark before cleaning it away.
# Multibyte marks are matched by anchored alternation, never a bracket class: a bracket class is
# byte-wise in a C locale and would half-match an em dash.
gawk -v CLEAN="$PUNCT_CLEAN" -v USEMAP="$PUNCT_PAUSE" -v NOTES="$NOTES_REQS" \
     -v PD="$PAUSE_DOT" -v PC="$PAUSE_COMMA" -v PDASH="$PAUSE_DASH" -v PE="$PAUSE_ELLIPSIS" \
     -v PSEMI="$PAUSE_SEMI" -v PCOLON="$PAUSE_COLON" -v PDEF="$PAUSE_DEFAULT" -v PS="$SENT_PAUSE" \
     -v PN="$NOTE_PAUSE" -v PLAN="$WORK/plan.raw" '
  function bare(t,   s) { s = t; gsub(/("|»|\x27|\)|\]|[ \t])+$/, "", s); return s }
  function dur(t,   s) {
    s = bare(t)
    if (!USEMAP) return (s ~ /(\.|!|\?|…|\.\.\.)$/) ? PS : 0
    if (s ~ /(…|\.\.\.)$/) return PE
    if (s ~ /(\.|!|\?)$/)  return PD
    if (s ~ /,$/)          return PC
    if (s ~ /(—|–|-)$/)    return PDASH
    if (s ~ /;$/)          return PSEMI
    if (s ~ /:$/)          return PCOLON
    return PDEF
  }
  function clean(t,   s) {
    if (!CLEAN) return t
    s = t
    sub(/[ \t]*(…|\.\.\.)[ \t]*$/, ".", s)
    sub(/[ \t]+(—|–|-)[ \t]*$/, "", s)
    return s
  }
  {
    i = index($0, ",\"text\":\"")
    if (i == 0) { print > (FILENAME == NOTES ? NOUT : MOUT); next }
    pre  = substr($0, 1, i + 8)
    text = substr($0, i + 9, length($0) - (i + 8) - 2)
    if (match($0, /"output":"[^"]*"/)) {
      out = substr($0, RSTART + 10, RLENGTH - 11)
      n = split(out, seg, "/"); base = seg[n]
      kind = (FILENAME == NOTES) ? "N" : "-"
      print base "\t" (kind == "N" ? PN : dur(text)) "\t" kind > PLAN
    }
    print pre clean(text) "\"}" > (FILENAME == NOTES ? NOUT : MOUT)
  }
' MOUT="$WORK/reqs.clean" NOUT="$WORK/notes.clean" "$WORK/reqs.ndjson" "$NOTES_REQS"
mv "$WORK/reqs.clean" "$WORK/reqs.ndjson"
if [ -s "$WORK/notes.clean" ]; then mv "$WORK/notes.clean" "$NOTES_REQS"; else rm -f "$WORK/notes.clean"; fi

# Headings: the first K chunks of each part, K from review/_titlecount.tsv. They are marked here and
# not in the pass above because "first K of the part" needs the chunks in c-number order, and the two
# request streams interleave. A missing _titlecount.tsv simply means no headings are bracketed.
sort "$WORK/plan.raw" | gawk -F'\t' -v OFS='\t' -v PT="$TITLE_PAUSE" '
  NR == FNR { K[$1] = $2; next }
  { p = gensub(/^p([0-9]+)_c.*/, "\\1", "g", $1); n[p]++
    if (n[p] <= K[p] + 0) { $2 = PT; $3 = "T" }
    print }
' "$REVIEW/_titlecount.tsv" - > "$WORK/plan.tsv" 2>/dev/null || cp "$WORK/plan.raw" "$WORK/plan.tsv"

# Engine: native Rust (ort) F5. The model DIR carries ckpt/vocab/ref; the daemon speaks the NDJSON
# {"text","output"} protocol and reads NFE from SOPDS_TTS_NFE ($NFE, default 32 — 16 sounds muffled).
[ -n "${F5BIN:-}" ] || { echo "fb2-to-f5.sh: set F5BIN to the sopds-tts-rs binary (native F5 synth)" >&2; exit 1; }
export SOPDS_TTS_NFE="$NFE"

# GPUS: how many cards to spread the daemons over. sopds-tts-rs builds CUDAExecutionProvider::default()
# and reads no device variable, so every process would land on device 0 and the other cards would idle
# while N daemons fight over the first. CUDA_VISIBLE_DEVICES remaps per process, so worker i gets card
# i % GPUS. Autodetected, because a wrong value is invisible in both directions — too low leaves cards
# idle, too high oversubscribes one, and both look like ordinary slowness.
# Note one daemon already saturates a card: WORKERS should equal GPUS, not exceed it.
# `|| true` and `if` rather than `&&`: with `set -e` a bare `[ x ] && y` aborts the script the moment
# the test is false, and `$(nvidia-smi …)` on a machine without it exits 127 — which is precisely the
# CPU-only host you would run JOIN_ONLY on.
if [ -z "${GPUS:-}" ]; then
  GPUS=$(nvidia-smi --query-gpu=name --format=csv,noheader 2>/dev/null | wc -l | tr -d ' ' || true)
fi
{ [ -n "${GPUS:-}" ] && [ "$GPUS" -ge 1 ] 2>/dev/null; } || GPUS=1
gpu_for() { if [ "$GPUS" -le 1 ]; then echo "${CUDA_VISIBLE_DEVICES:-0}"; else echo $(( $1 % GPUS )); fi; }
if [ "$GPUS" -gt 1 ]; then echo "→ $GPUS GPUs: workers pinned round-robin (worker i → card i % $GPUS)"; fi

SECONDS=0; pids=()
if [ "$JOIN_ONLY" = 1 ]; then
  echo "→ JOIN_ONLY: reusing $(find "$WORK" -maxdepth 1 -name 'p*_c*.wav' | wc -l | tr -d ' ') wavs in $WORK"
else
  [ "$NNOTES" -gt 0 ] && echo "→ synthesizing $NMAIN narration + $NNOTES footnote chunks (native rust nfe=$NFE)" \
                      || echo "→ synthesizing $N chunks on $WORKERS daemon(s) (native rust nfe=$NFE)"
  for ((i=0;i<WORKERS;i++)); do
    gawk -v W="$WORKERS" -v id="$i" 'NR%W==id' "$WORK/reqs.ndjson" > "$WORK/shard_$i"
    SOPDS_TTS_THREADS="${THREADS:-0}" CUDA_VISIBLE_DEVICES="$(gpu_for "$i")" \
      "$F5BIN" "$F5MODEL" < "$WORK/shard_$i" > "$WORK/resp_$i" 2>"$WORK/dlog_$i" &
    pids+=($!)
  done
  # Footnotes → their own voice. With NOTES_VOICES/manifest.txt each footnote gets a different voice,
  # rotated in book order; the voices run one at a time inside a subshell so memory stays bounded at
  # one note daemon plus WORKERS narration daemons. Pinned to the last card, since worker 0 has the first.
  if [ "$NNOTES" -gt 0 ]; then
    if [ -n "${NOTES_VOICES:-}" ] && [ -f "$NOTES_VOICES/manifest.txt" ]; then
      VOICES=(); while IFS= read -r v; do [ -n "$v" ] && VOICES+=("$v"); done < "$NOTES_VOICES/manifest.txt"
      K=${#VOICES[@]}; vi=0
      while IFS= read -r line; do
        printf '%s\n' "$line" >> "$WORK/notes_${VOICES[$((vi % K))]}.ndjson"; vi=$((vi+1))
      done < "$NOTES_REQS"
      echo "  ($vi footnote chunks → $K voices: ${VOICES[*]})"
      (
        for v in "${VOICES[@]}"; do
          [ -s "$WORK/notes_$v.ndjson" ] || continue
          SOPDS_TTS_THREADS="${THREADS:-0}" CUDA_VISIBLE_DEVICES="$(gpu_for $((GPUS - 1)))" \
            "$F5BIN" "$NOTES_VOICES/$v" < "$WORK/notes_$v.ndjson" >> "$WORK/resp_notes" 2>>"$WORK/dlog_notes"
        done
      ) &
      pids+=($!)
    else
      echo "  ($NNOTES footnote chunks → $F5MODEL_NOTES)"
      SOPDS_TTS_THREADS="${THREADS:-0}" CUDA_VISIBLE_DEVICES="$(gpu_for $((GPUS - 1)))" \
        "$F5BIN" "$F5MODEL_NOTES" < "$NOTES_REQS" > "$WORK/resp_notes" 2>>"$WORK/dlog_notes" &
      pids+=($!)
    fi
  fi
  while :; do
    alive=0; for pd in "${pids[@]}"; do kill -0 "$pd" 2>/dev/null && { alive=1; break; }; done
    done=$(find "$WORK" -maxdepth 1 -name 'p*_c*.wav' | wc -l | tr -d ' ')
    printf '\r\033[K  %d/%d  %dm%02ds' "$done" "$N" $((SECONDS/60)) $((SECONDS%60))
    [ "$alive" -eq 0 ] && break; sleep 3
  done
  wait "${pids[@]}" 2>/dev/null || true
  _made=$(find "$WORK" -maxdepth 1 -name 'p*_c*.wav'|wc -l|tr -d ' ')
  printf '\r\033[K  %d/%d done in %dm%02ds\n' "$_made" "$N" $((SECONDS/60)) $((SECONDS%60))
  # Do not let a dead daemon look like a finished run. Without this the script printed "0/N done" and
  # then "✓ done" with no mp3 and no error — which is exactly how a missing libonnxruntime.so.1 read
  # as success. The daemon log says why.
  if [ "$_made" -lt "$N" ]; then
    echo "✗ synthesis produced $_made of $N chunks — see $WORK/dlog_* :" >&2
    tail -n 5 "$WORK"/dlog_* 2>/dev/null >&2 || true
    if [ "$_made" -eq 0 ]; then exit 1; fi
  fi
fi

echo "→ joining parts…"
# Silence files at the synthesized rate — the concat demuxer wants matching streams, so the rate is
# read off a produced wav rather than assumed. One file per distinct pause length in the plan.
_firstwav=$(find "$WORK" -maxdepth 1 -name 'p*_c*.wav' | sort | head -1)
[ -n "$_firstwav" ] || { echo "no wavs in $WORK — nothing to join" >&2; exit 1; }
_sr=$(ffprobe -v error -select_streams a:0 -show_entries stream=sample_rate -of csv=p=0 "$_firstwav" 2>/dev/null || true)
for d in $(cut -f2 "$WORK/plan.tsv" | sort -u) "$TITLE_PAUSE"; do
  awk "BEGIN{exit !($d > 0)}" || continue
  [ -f "$WORK/_sil_$d.wav" ] || ffmpeg -y -hide_banner -loglevel error -f lavfi \
    -i "anullsrc=r=${_sr:-24000}:cl=mono" -t "$d" -c:a pcm_s16le "$WORK/_sil_$d.wav"
done

# Footnote chimes: the warm two-tone airport bell — falling A5→E5 "пім-пуум" INTO a note run, rising
# E5→A5 back to narration. Prefer the saved/tweakable files under $F5_HOME/chimes; regenerate the
# built-in bell only if absent. CHIME=0 disables.
CHIME=${CHIME:-1}; CHIME_IN=""; CHIME_OUT=""
if [ "$NNOTES" -gt 0 ] && [ "$CHIME" != 0 ]; then
  _mkbell() { # <freq_short> <freq_long> <out> — two-tone decaying bell (exp decay, lowpass + light echo)
    ffmpeg -y -v error -f lavfi -i "sine=frequency=$1:duration=0.6" -af "volume='exp(-7*t)':eval=frame,afade=t=in:d=0.008"   -ar "${_sr:-24000}" -ac 1 "$WORK/_ch1.wav"
    ffmpeg -y -v error -f lavfi -i "sine=frequency=$2:duration=1.1" -af "volume='exp(-3.5*t)':eval=frame,afade=t=in:d=0.008" -ar "${_sr:-24000}" -ac 1 "$WORK/_ch2.wav"
    printf "file '%s/_ch1.wav'\nfile '%s/_ch2.wav'\n" "$WORK" "$WORK" > "$WORK/_chl.txt"
    ffmpeg -y -v error -f concat -safe 0 -i "$WORK/_chl.txt" -af "lowpass=f=6500,aecho=0.8:0.6:60:0.25,volume=0.5" -ar "${_sr:-24000}" -ac 1 -c:a pcm_s16le "$3"
  }
  CHIME_IN=${CHIME_IN_OVERRIDE:-$F5_HOME/chimes/chime_in.wav};   [ -f "$CHIME_IN" ]  || { CHIME_IN=$WORK/chime_in.wav;   _mkbell 880 659 "$CHIME_IN"; }
  CHIME_OUT=${CHIME_OUT_OVERRIDE:-$F5_HOME/chimes/chime_out.wav}; [ -f "$CHIME_OUT" ] || { CHIME_OUT=$WORK/chime_out.wav; _mkbell 659 880 "$CHIME_OUT"; }
fi

# F5 pads each chunk with silence and a phrase-final cadence; trimming both ends is what makes the
# deterministic pause the ONLY gap at a seam. Costs one ffmpeg per chunk. TRIM_SILENCE=0 disables.
TRIMAF="silenceremove=start_periods=1:start_threshold=-50dB,areverse,silenceremove=start_periods=1:start_threshold=-50dB,areverse"
trimw() {
  [ "$TRIM_SILENCE" = 1 ] || { echo "$1"; return; }
  local t="$WORK/t_$(basename "$1")"
  [ -f "$t" ] && { echo "$t"; return; }
  ffmpeg -nostdin -y -hide_banner -loglevel error -i "$1" -af "$TRIMAF" -ar "${_sr:-24000}" -ac 1 -c:a pcm_s16le "$t" && echo "$t" || echo "$1"
}

while IFS=$'\t' read -r pp safe title; do
  files=$(find "$WORK" -maxdepth 1 -name "p${pp}_c*.wav" ! -name 't_*' | sort); [ -n "$files" ] || continue
  : > "$WORK/files_$pp.tsv"
  while IFS= read -r wf; do
    [ -n "$wf" ] || continue
    printf '%s\t%s\n' "$(basename "$wf")" "$(trimw "$wf")" >> "$WORK/files_$pp.tsv"
  done <<< "$files"
  # One pass turns the plan into the concat list: chimes bracket each run of footnotes, a heading is
  # bracketed by TITLE_PAUSE on both sides, every other chunk is followed by the pause its own final
  # mark earned. Two adjacent silences collapse into one (two headings in a row share their pause),
  # and a silence is never left dangling at the end of a part.
  gawk -F'\t' -v Q="'" -v W="$WORK" -v PT="$TITLE_PAUSE" -v CI="$CHIME_IN" -v CO="$CHIME_OUT" '
    function emit(x, s) { out[++k] = x; sil[k] = s }
    function sfile(d) { return W "/_sil_" d ".wav" }
    NR == FNR { pause[$1] = $2; kind[$1] = $3; next }
    {
      base = $1; path = $2
      kd = (base in kind) ? kind[base] : "-"
      d  = (base in pause) ? pause[base] : 0
      if (CI != "" && kd == "N" && prev != "N") emit("file " Q CI Q, 0)
      # Leaving a footnote: the closing chime comes FIRST and the pause after it, so the bell ends the
      # aside and the silence separates it from the narration — not the other way round.
      if (kd != "N" && prev == "N") { if (CO != "") emit("file " Q CO Q, 0)
                                      if (np + 0 > 0) { emit("file " Q sfile(np) Q, 1); np = 0 } }
      if (kd == "T" && PT + 0 > 0) emit("file " Q sfile(PT) Q, 1)
      emit("file " Q path Q, 0)
      if (kd == "N")                    np = d          # deferred until the chime closes the run
      else if (kd == "T" && PT + 0 > 0) emit("file " Q sfile(PT) Q, 1)
      else if (d + 0 > 0)               emit("file " Q sfile(d) Q, 1)
      prev = kd
    }
    END {
      if (prev == "N") { if (CO != "") emit("file " Q CO Q, 0)
                         if (np + 0 > 0) emit("file " Q sfile(np) Q, 1) }
      n = 0
      for (i = 1; i <= k; i++) {
        if (sil[i] && n > 0 && keepsil[n]) continue
        keep[++n] = out[i]; keepsil[n] = sil[i]
      }
      if (n > 0 && keepsil[n]) n--
      for (i = 1; i <= n; i++) print keep[i]
    }' "$WORK/plan.tsv" "$WORK/files_$pp.tsv" > "$WORK/list_$pp.txt"
  [ -s "$WORK/list_$pp.txt" ] || continue
  o="$OUT/${pp}_${safe}.mp3"
  ffmpeg -nostdin -y -hide_banner -loglevel error -f concat -safe 0 -i "$WORK/list_$pp.txt" -c:a libmp3lame -b:a 64k -ac 1 "$o"
  echo "  ✓ $o ($(du -h "$o"|cut -f1))"
done < "$REVIEW/_titles.tsv"
echo "✓ done → $OUT"
