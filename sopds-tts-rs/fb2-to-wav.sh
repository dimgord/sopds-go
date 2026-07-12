#!/usr/bin/env bash
# fb2-to-wav.sh — convert a whole FB2 book to one audio file via sopds-tts-rs.
#
#   ./fb2-to-wav.sh <book.fb2> <model.onnx> [output]
#
# Extracts the main body text, splits it into ~600-character sentence-grouped chunks
# (Piper/VITS attention is O(n²) — big chunks OOM the GPU, see PROGRESS Rev 81),
# synthesizes them across WORKERS resident daemons (default 4 — on a multi-core CPU that's
# ~2×; Apple Silicon has no usable GPU for this model), and joins them.
#
# Output format follows the extension (default .mp3): mp3 / m4b / m4a / opus / ogg are
# encoded (speech-grade bitrate); .wav is copied but is hard-capped at 4 GB by the WAV
# format — a longer book auto-switches to .mp3 so you never get a broken file.
#
# Runtime deps (espeak-ng, ffmpeg, jq, xmllint, GNU sed/awk) are pulled from nixpkgs
# automatically when `nix` is available, so on a nix box nothing needs preinstalling.
#
#   WORKERS=6 MAXCHARS=800 ./fb2-to-wav.sh book.fb2 ru_RU-irina-medium.onnx out.m4b
set -euo pipefail

# Re-exec once inside a nix shell so all tools (incl. GNU sed/awk) are on PATH.
if [ -z "${_FB2_NIX:-}" ] && command -v nix >/dev/null 2>&1; then
  export _FB2_NIX=1
  exec nix shell nixpkgs#espeak-ng nixpkgs#ffmpeg nixpkgs#jq \
                 nixpkgs#libxml2 nixpkgs#gnused nixpkgs#gawk nixpkgs#coreutils \
                 -c "$0" "$@"
fi

FB2=${1:?usage: fb2-to-wav.sh <book.fb2> <model.onnx> [output.wav]}
MODEL=${2:?usage: fb2-to-wav.sh <book.fb2> <model.onnx> [output.wav]}
OUT=${3:-${FB2%.fb2}.mp3}
# Chunk size is in CHARACTERS (gawk length() counts runes) — this bounds the phoneme
# count, which is what drives VITS attention memory. Keep ≤~700: on an 8 GB GPU a
# ~1150-char chunk OOMs. (MAXBYTES kept as a legacy alias.)
MAXCHARS=${MAXCHARS:-${MAXBYTES:-600}}
BIN=${SOPDS_TTS_BIN:-"$(cd "$(dirname "$0")" && pwd)/target/release/sopds-tts-rs"}

[ -x "$BIN" ]   || { echo "TTS binary not found: $BIN (build: cargo build --release, or set SOPDS_TTS_BIN)"; exit 1; }
[ -f "$MODEL" ] || { echo "model not found: $MODEL"; exit 1; }
[ -f "$MODEL.json" ] || echo "warning: $MODEL.json (Piper config) not found next to the model" >&2

fmt_dur() { # seconds -> "1h02m" / "3m05s" / "12s"
  local s=$1
  if   [ "$s" -ge 3600 ]; then printf '%dh%02dm' $((s/3600)) $(((s%3600)/60))
  elif [ "$s" -ge 60   ]; then printf '%dm%02ds' $((s/60)) $((s%60))
  else                         printf '%ds' "$s"; fi
}

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

echo "→ extracting text from $FB2"
# Main body only (skip <body name="notes"> footnotes). Flatten, then one sentence per line.
xmllint --xpath '//*[local-name()="body"][not(@name)]//text()' "$FB2" 2>/dev/null \
  | tr '\n' ' ' \
  | sed -E 's/([.!?]["»”)]*) +/\1\n/g' \
  | awk -v max="$MAXCHARS" '
      { gsub(/^ +| +$/,""); if ($0=="") next
        if (buf=="")                          buf=$0
        else if (length(buf)+1+length($0)<=max) buf=buf" "$0
        else { print buf; buf=$0 } }
      END { if (buf!="") print buf }' > "$WORK/chunks.txt"

N=$(wc -l < "$WORK/chunks.txt" | tr -d ' ')
[ "${N:-0}" -gt 0 ] || { echo "no text extracted from $FB2"; exit 1; }
echo "→ $N chunks (~${MAXCHARS} chars each)"

# One NDJSON request per chunk (jq handles all escaping; zero-padded name keeps concat order).
jq -Rc --arg dir "$WORK" \
   '{text: ., output: ($dir + "/c" + (("00000"+(input_line_number|tostring))[-5:]) + ".wav")}' \
   "$WORK/chunks.txt" > "$WORK/reqs.ndjson"

# Parallelism: WORKERS daemons, each capped to ~cores/WORKERS ORT threads so they don't
# oversubscribe. Apple Silicon has no usable GPU for this model (CoreML can't run VITS's
# dynamic output length), so CPU is the ceiling — ~4 workers ≈ 2× (bounded by the
# performance-core count). On a real GPU box keep WORKERS low (VRAM per resident model).
WORKERS=${WORKERS:-4}
[ "$WORKERS" -lt 1 ]    && WORKERS=1
[ "$WORKERS" -gt "$N" ] && WORKERS=$N
NCPU=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
THREADS=${SOPDS_TTS_THREADS:-$(( NCPU/WORKERS > 0 ? NCPU/WORKERS : 1 ))}
echo "→ synthesizing on $WORKERS daemon(s) × $THREADS threads…"

for ((i=0; i<WORKERS; i++)); do          # round-robin keeps chunk sizes balanced per shard
  awk -v W="$WORKERS" -v id="$i" 'NR % W == id' "$WORK/reqs.ndjson" > "$WORK/shard_$i"
done
SECONDS=0; pids=()
for ((i=0; i<WORKERS; i++)); do
  SOPDS_TTS_THREADS="$THREADS" "$BIN" "$MODEL" < "$WORK/shard_$i" > "$WORK/resp_$i" 2>/dev/null &
  pids+=($!)
done

while :; do                              # progress by counting produced WAVs across all daemons
  alive=0; for p in "${pids[@]}"; do kill -0 "$p" 2>/dev/null && { alive=1; break; }; done
  done=$(find "$WORK" -maxdepth 1 -name 'c*.wav' | wc -l | tr -d ' '); pct=$((done*100/N))
  if [ "$done" -gt 0 ] && [ "$SECONDS" -gt 0 ]; then
    eta=$(( (N-done)*SECONDS/done ))
    printf '\r\033[K  %d/%d (%d%%)  %s elapsed  ETA %s' "$done" "$N" "$pct" "$(fmt_dur "$SECONDS")" "$(fmt_dur "$eta")"
  else
    printf '\r\033[K  %d/%d (%d%%)' "$done" "$N" "$pct"
  fi
  [ "$alive" -eq 0 ] && break
  sleep 1
done
wait "${pids[@]}" 2>/dev/null || true
ok=$(cat "$WORK"/resp_* 2>/dev/null | grep -c '"ok":true'  || true)
fail=$(cat "$WORK"/resp_* 2>/dev/null | grep -c '"ok":false' || true)
printf '\r\033[K  %d/%d done in %s\n' "$ok" "$N" "$(fmt_dur "$SECONDS")"
[ "${fail:-0}" -eq 0 ] || { echo "  ($fail chunk(s) failed — skipped in output):" >&2
  cat "$WORK"/resp_* 2>/dev/null | grep '"ok":false' | jq -r '.error' 2>/dev/null | sort | uniq -c | head >&2; }

ls "$WORK"/c*.wav >/dev/null 2>&1 || { echo "no audio produced"; exit 1; }
for f in "$WORK"/c*.wav; do printf "file '%s'\n" "$f"; done > "$WORK/list.txt"

# Encode by output extension. WAV's 32-bit size fields cap it at 4 GB — a longer book
# would get a broken header, so auto-switch to MP3 past that.
ext=${OUT##*.}
if [ "$ext" = wav ]; then
  kb=$(du -ck "$WORK"/c*.wav 2>/dev/null | tail -1 | awk '{print $1}')
  if [ "${kb:-0}" -gt 4000000 ]; then
    echo "note: audio is ~$((kb/1024)) MB — over WAV's 4 GB limit → writing MP3 instead" >&2
    OUT="${OUT%.*}.mp3"; ext=mp3
  fi
fi
case "$ext" in
  wav)         enc=(-c copy) ;;                       # lossless, ≤4 GB
  m4a|m4b|aac) enc=(-c:a aac -b:a 64k -ac 1) ;;
  opus|ogg)    enc=(-c:a libopus -b:a 32k -ac 1) ;;
  *)           enc=(-c:a libmp3lame -b:a 64k -ac 1) ;; # mp3 (default + fallback)
esac
echo "→ writing $OUT"
ffmpeg -hide_banner -loglevel error -f concat -safe 0 -i "$WORK/list.txt" "${enc[@]}" "$OUT"
echo "✓ $OUT ($(du -h "$OUT" | cut -f1))"
