#!/usr/bin/env bash
# fb2-to-f5.sh — narrate an FB2 book with F5-TTS in your cloned voice, one MP3 per part.
#
#   fb2-to-f5.sh <book.fb2> [out_dir]
#
# Per top-level <section> (= "part"): extract text -> ~MAXCHARS sentence chunks ->
# RUAccent stress (separate venv) -> synthesize across WORKERS resident F5 daemons ->
# concat into out_dir/NN_<title>.mp3. Model is loaded once per daemon (not per chunk).
#
# env: NFE=16  WORKERS=1  MAXCHARS=250  DEVICE=cpu  PARTS="1 2 3 4" (subset for testing)
#      F5_HOME (spike dir), REF, REF_TEXT (voice reference), CKPT, VOCAB
#
# BRIDGE ONLY — the native Rust (ort) F5 replaces this Python; see docs/decisions/001.
set -euo pipefail

FB2=${1:?usage: fb2-to-f5.sh <book.fb2> [out_dir]}
OUT=${2:-./f5-book}
F5_HOME=${F5_HOME:-~/src/f5-spike}
REF=${REF:-$F5_HOME/ab/ref_fixed.wav}
REF_TEXT=${REF_TEXT:-$F5_HOME/ab/ref_fixed.txt}
CKPT=${CKPT:-$F5_HOME/ru-model/model_v2.safetensors}
VOCAB=${VOCAB:-$F5_HOME/ru-model/vocab.txt}
NFE=${NFE:-16}; WORKERS=${WORKERS:-1}; MAXCHARS=${MAXCHARS:-250}; DEVICE=${DEVICE:-cpu}
F5PY="$F5_HOME/f5env/bin/python"
RUPY="$F5_HOME/ruaccent-env/bin/python"

mkdir -p "$OUT"
WORK=$(mktemp -d); trap 'rm -rf "$WORK"' EXIT
xp() { xmllint --xpath "$1" "$FB2" 2>/dev/null; }             # xpath helper
sect() { echo "//*[local-name()=\"body\"][not(@name)]/*[local-name()=\"section\"][$1]"; }

NPARTS=$(xp "count($(sect '*' | sed 's/\[\*\]//'))")          # count top-level sections
PARTS=${PARTS:-$(seq 1 "${NPARTS%.*}")}
echo "→ book has ${NPARTS%.*} parts; doing: $PARTS  (nfe=$NFE workers=$WORKERS chars=$MAXCHARS $DEVICE)"

# 1) extract + chunk every selected part; record each chunk's part in parts.txt (parallel to chunks.txt)
: > "$WORK/chunks.txt"; : > "$WORK/parts.txt"; : > "$WORK/titles.txt"
for p in $PARTS; do
  title=$(xp "$(sect "$p")/*[local-name()=\"title\"]" | sed 's/<[^>]*>//g' | tr '\n' ' ' | sed -E 's/ +/ /g; s/^ | $//g' | cut -c1-40)
  printf '%s\t%s\n' "$p" "${title:-part $p}" >> "$WORK/titles.txt"
  xp "$(sect "$p")//text()" | tr '\n' ' ' | sed -E 's/([.!?]["»)]*) +/\1\n/g' \
    | gawk -v max="$MAXCHARS" '{gsub(/^ +| +$/,"");if($0=="")next;if(b=="")b=$0;else if(length(b)+1+length($0)<=max)b=b" "$0;else{print b;b=$0}}END{if(b!="")print b}' \
    | while IFS= read -r line; do printf '%s\n' "$line" >> "$WORK/chunks.txt"; printf '%s\n' "$p" >> "$WORK/parts.txt"; done
done
N=$(wc -l < "$WORK/chunks.txt" | tr -d ' ')
[ "$N" -gt 0 ] || { echo "no text"; exit 1; }
echo "→ $N chunks total"

# 2) RUAccent stress (separate venv, robust fallback)
echo "→ stressing (RUAccent)…"
"$RUPY" "$F5_HOME/ruaccent_batch.py" < "$WORK/chunks.txt" > "$WORK/stressed.txt" 2>"$WORK/ru.err" || true
[ "$(wc -l < "$WORK/stressed.txt")" -eq "$N" ] || { echo "stress line-count mismatch"; exit 1; }

# 3) NDJSON: output = WORK/pPP_cNNNNN.wav  (PP=part, NNNNN=global order for concat sort)
"$F5PY" - "$WORK" "$WORK/parts.txt" "$WORK/stressed.txt" > "$WORK/reqs.ndjson" <<'PY'
import json, sys
work, pf, sf = sys.argv[1], sys.argv[2], sys.argv[3]
parts = open(pf, encoding="utf-8").read().splitlines()
texts = open(sf, encoding="utf-8").read().splitlines()
for i, (p, t) in enumerate(zip(parts, texts), 1):
    print(json.dumps({"text": t, "output": f"{work}/p{int(p):02d}_c{i:05d}.wav"}, ensure_ascii=False))
PY

# 4) WORKERS resident F5 daemons over round-robin shards
echo "→ synthesizing on $WORKERS daemon(s)…"
SECONDS=0; pids=()
for ((i=0;i<WORKERS;i++)); do
  gawk -v W="$WORKERS" -v id="$i" 'NR%W==id' "$WORK/reqs.ndjson" > "$WORK/shard_$i"
  "$F5PY" "$F5_HOME/f5_daemon.py" --ckpt "$CKPT" --vocab "$VOCAB" --ref "$REF" \
     --ref-text "$REF_TEXT" --nfe "$NFE" --device "$DEVICE" \
     < "$WORK/shard_$i" > "$WORK/resp_$i" 2>"$WORK/dlog_$i" &
  pids+=($!)
done
while :; do
  alive=0; for pd in "${pids[@]}"; do kill -0 "$pd" 2>/dev/null && { alive=1; break; }; done
  done=$(find "$WORK" -maxdepth 1 -name 'p*_c*.wav' | wc -l | tr -d ' ')
  printf '\r\033[K  %d/%d chunks  %dm%02ds' "$done" "$N" $((SECONDS/60)) $((SECONDS%60))
  [ "$alive" -eq 0 ] && break; sleep 3
done
wait "${pids[@]}" 2>/dev/null || true
fails=$(cat "$WORK"/resp_* 2>/dev/null | grep -c '"ok": false' || true)
printf '\r\033[K  %d/%d chunks done in %dm%02ds (%s stress-fallbacks, %s synth-fails)\n' \
  "$(find "$WORK" -maxdepth 1 -name 'p*_c*.wav' | wc -l | tr -d ' ')" "$N" $((SECONDS/60)) $((SECONDS%60)) \
  "$(grep -c stress-fallback "$WORK/ru.err" 2>/dev/null || echo 0)" "${fails:-0}"

# 5) concat per part -> mp3
echo "→ joining parts…"
while IFS=$'\t' read -r p title; do
  safe=$(printf '%s' "$title" | tr ' /' '__' | tr -cd 'A-Za-z0-9_А-Яа-яЁё.-')
  files=$(find "$WORK" -maxdepth 1 -name "$(printf 'p%02d_c*.wav' "$p")" | sort)
  [ -n "$files" ] || continue
  echo "$files" | sed "s|^|file '|; s|$|'|" > "$WORK/list_$p.txt"
  out=$(printf '%s/%02d_%s.mp3' "$OUT" "$p" "${safe:-part}")
  ffmpeg -y -hide_banner -loglevel error -f concat -safe 0 -i "$WORK/list_$p.txt" -c:a libmp3lame -b:a 64k -ac 1 "$out"
  echo "  ✓ $out ($(du -h "$out" | cut -f1))"
done < "$WORK/titles.txt"
echo "✓ done → $OUT"
