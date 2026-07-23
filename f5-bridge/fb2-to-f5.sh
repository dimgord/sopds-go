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
# F5PY: a plain python3 for the JSON glue + reviewer scripts (stdlib only). env override wins.
F5PY="${F5PY:-$F5_HOME/f5env/bin/python}"
# Stress engine: native Rust `sopds-tts-rs stress` (STRESSBIN, else the F5BIN binary — it's the same
# binary). Needs RUACCENT_HOME (the dictionary + nn models dir). Accepts --fix / --dump-homographs.
export RUACCENT_HOME="${RUACCENT_HOME:-$HOME/.cache/ruaccent}"
_stressbin="${STRESSBIN:-${F5BIN:-}}"
[ -n "$_stressbin" ] || { echo "fb2-to-f5.sh: set STRESSBIN or F5BIN to the sopds-tts-rs binary" >&2; exit 1; }
STRESS=("$_stressbin" stress)
REVIEW="$OUT/review"
mkdir -p "$OUT" "$REVIEW"
# `|| true`: an empty node-set (e.g. a section with no <title>) makes xmllint exit non-zero,
# which under set -e+pipefail would kill the script — tolerate it, callers handle empty output.
xp() { xmllint --xpath "$1" "$FB2" 2>/dev/null || true; }
# Section addressing (namespace-agnostic). Two levels: a top-level section [P], or its nested
# section [P]/[S] (S is the POSITION within part P, not a global chapter number).
BODYSEC='//*[local-name()="body"][not(@name)]/*[local-name()="section"]'
nodeXP()  { if [ -n "${2:-}" ]; then echo "${BODYSEC}[$1]/*[local-name()=\"section\"][$2]"; else echo "${BODYSEC}[$1]"; fi; }
nTop()    { xp "count($BODYSEC)" | cut -d. -f1; }
nSub()    { xp "count(${BODYSEC}[$1]/*[local-name()=\"section\"])" | cut -d. -f1; }
titleOf() { xp "$1/*[local-name()=\"title\"]" | sed 's/<[^>]*>//g' | tr '\n' ' ' | sed -E 's/ +/ /g; s/^ | $//g' | cut -c1-40; }
# COMBINE — MP3 granularity for a WHOLE-part selection: 1 = one MP3 per top-level section (all its
# nested sections joined); 2 = one MP3 per nested section. (An explicit "P:S"/"P:S1-S2" is always per
# nested section regardless.) A part with no nested sections stays a single unit either way.
COMBINE=${COMBINE:-1}
emitPart() {  # $1 = P — expand a whole-part selection per COMBINE
  if [ "$COMBINE" = 2 ] && [ "$(nSub "$1")" -gt 0 ]; then
    local s; for s in $(seq 1 "$(nSub "$1")"); do echo "$1:$s"; done
  else echo "$1"; fi
}
# Expand the PARTS selector into one unit per line: "P" (whole top section) or "P:S" (nested section).
#   PARTS syntax: "P" | "P1-P2" | "P:S" | "P:S1-S2", space-separated. Empty ⇒ all top-level sections.
partUnits() {
  if [ -z "${PARTS:-}" ]; then local p; for p in $(seq 1 "$(nTop)"); do emitPart "$p"; done; return; fi
  local tok p sr s s1 s2 p1 p2
  for tok in $PARTS; do case "$tok" in
    *:*) p="${tok%%:*}"; sr="${tok#*:}"
         case "$sr" in
           *-*) s1="${sr%%-*}"; s2="${sr##*-}"; for s in $(seq "$s1" "$s2"); do echo "$p:$s"; done ;;
           *)   echo "$p:$sr" ;;
         esac ;;
    *-*) p1="${tok%%-*}"; p2="${tok##*-}"; for p in $(seq "$p1" "$p2"); do emitPart "$p"; done ;;
    *)   emitPart "$tok" ;;
  esac; done
}

# ---- STRESS phase: produce per-unit reviewable stressed text --------------------------------
if [ "$MODE" = stress ] || [ "$MODE" = all ]; then
  NTOP=$(nTop)
  # Structure map — shows part→nested boundaries so you can pick PARTS selectors (P / P:S / P:S1-S2).
  echo "→ book: $NTOP top-level section(s)"
  for _p in $(seq 1 "$NTOP"); do echo "     [$_p] $(titleOf "$(nodeXP "$_p")")  → $(nSub "$_p") nested"; done
  [ "$NTOP" = 1 ] && [ "$(nSub 1)" = 0 ] && echo "  (flat book — one section, no nested; P:S selectors N/A. Heading-split mode TBD.)"
  echo "→ stressing units: $(partUnits | tr '\n' ' ') (chars≤$MAXCHARS)"
  : > "$REVIEW/_titles.tsv"
  while read -r unit; do
    p="${unit%%:*}"; s=""; case "$unit" in *:*) s="${unit#*:}";; esac
    node=$(nodeXP "$p" "$s")
    title=$(titleOf "$node")
    if [ -n "$s" ]; then id=$(printf '%02d.%02d' "$p" "$s"); def="part_${p}_${s}"; else id=$(printf '%02d' "$p"); def="part_$p"; fi
    safe=$(printf '%s' "${title:-$def}" | tr ' /' '__' | tr -cd 'A-Za-z0-9_А-Яа-яЁё.-')
    printf '%s\t%s\t%s\n' "$id" "$safe" "$title" >> "$REVIEW/_titles.tsv"
    xp "$node//text()" | tr '\n' ' ' | sed -E 's/([.!?]["»)]*) +/\1\n/g' \
      | gawk -v max="$MAXCHARS" '{gsub(/^ +| +$/,"");if($0=="")next;if(b=="")b=$0;else if(length(b)+1+length($0)<=max)b=b" "$0;else{print b;b=$0}}END{if(b!="")print b}' \
      > "$REVIEW/${id}_${safe}.raw.txt"
    "${STRESS[@]}" ${FIX:+--fix "$FIX"} \
      < "$REVIEW/${id}_${safe}.raw.txt" \
      > "$REVIEW/${id}_${safe}.txt" 2>>"$REVIEW/_ruaccent.log"
    echo "  ✓ $unit → ${id}: $(wc -l < "$REVIEW/${id}_${safe}.txt") chunks — $title"
  done < <(partUnits)
  # Ambiguous-homograph report: only flag ё-restorations on genuine homographs (берет, десны, …),
  # not the always-ё words (ещё, всё, её). These are the ones worth eyeballing in the review text.
  "${STRESS[@]}" --dump-homographs "$REVIEW/_homographs.txt" </dev/null 2>/dev/null || true
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
    "$F5PY" -c 'import json,sys;print(json.dumps({"text":sys.argv[1],"output":sys.argv[2]},ensure_ascii=False))' "$line" "$out"
  done < "$f"
done < "$REVIEW/_titles.tsv" > "$WORK/reqs.ndjson"
N=$(wc -l < "$WORK/reqs.ndjson" | tr -d ' ')
[ "$N" -gt 0 ] || { echo "no stressed text — run MODE=stress first"; exit 1; }
# Engine: native Rust (ort) F5 when F5BIN is set (the worker sets it) — the model DIR carries the
# ckpt/vocab/ref/nfe, and the daemon speaks the same NDJSON {"text","output"} protocol. Otherwise fall
# back to the legacy Python (torch) f5_daemon.py, which needs an F5PY with torch/f5_tts.
# Native F5 reads NFE from SOPDS_TTS_NFE (the model dir has no nfe baked in); the legacy py daemon
# takes --nfe. Both honor $NFE (default 16 — the F5 default, ~2x faster than 32).
export SOPDS_TTS_NFE="$NFE"
echo "→ synthesizing $N chunks on $WORKERS daemon(s) ($([ -n "${F5BIN:-}" ] && echo "native rust" || echo "py torch $DEVICE") nfe=$NFE)"

SECONDS=0; pids=()
for ((i=0;i<WORKERS;i++)); do
  gawk -v W="$WORKERS" -v id="$i" 'NR%W==id' "$WORK/reqs.ndjson" > "$WORK/shard_$i"
  if [ -n "${F5BIN:-}" ]; then
    "$F5BIN" "$F5MODEL" < "$WORK/shard_$i" > "$WORK/resp_$i" 2>"$WORK/dlog_$i" &
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
