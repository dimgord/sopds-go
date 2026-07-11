#!/usr/bin/env bash
# fb2-to-wav.sh — convert a whole FB2 book to a single WAV via sopds-tts-rs.
#
#   ./fb2-to-wav.sh <book.fb2> <model.onnx> [output.wav]
#
# Extracts the main body text, splits it into ~900-byte sentence-grouped chunks
# (Piper/VITS attention is O(n²) — big chunks OOM the GPU, see PROGRESS Rev 81),
# synthesizes them all through ONE resident daemon (model loaded once), and
# concatenates the chunk WAVs into a single file.
#
# Runtime deps (espeak-ng, ffmpeg, jq, xmllint, GNU sed/awk) are pulled from nixpkgs
# automatically when `nix` is available, so on a nix box nothing needs preinstalling.
#
#   MAXBYTES=800 ./fb2-to-wav.sh book.fb2 ~/piper-models/ru_RU-irina-medium.onnx out.wav
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
OUT=${3:-${FB2%.fb2}.wav}
MAXBYTES=${MAXBYTES:-900}
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
  | awk -v max="$MAXBYTES" '
      { gsub(/^ +| +$/,""); if ($0=="") next
        if (buf=="")                          buf=$0
        else if (length(buf)+1+length($0)<=max) buf=buf" "$0
        else { print buf; buf=$0 } }
      END { if (buf!="") print buf }' > "$WORK/chunks.txt"

N=$(wc -l < "$WORK/chunks.txt" | tr -d ' ')
[ "${N:-0}" -gt 0 ] || { echo "no text extracted from $FB2"; exit 1; }
echo "→ $N chunks (~${MAXBYTES}B each); synthesizing on one resident daemon…"

# One NDJSON request per chunk (jq handles all escaping; zero-padded name keeps concat order).
jq -Rc --arg dir "$WORK" \
   '{text: ., output: ($dir + "/c" + (("00000"+(input_line_number|tostring))[-5:]) + ".wav")}' \
   "$WORK/chunks.txt" > "$WORK/reqs.ndjson"

ok=0; fail=0; SECONDS=0
while IFS= read -r line; do
  if [ "$(printf '%s' "$line" | jq -r '.ok')" = "true" ]; then ok=$((ok+1))
  else fail=$((fail+1)); printf '\r\033[Kchunk error: %s\n' "$(printf '%s' "$line" | jq -r '.error')" >&2; fi
  done=$((ok+fail)); pct=$((done*100/N))
  if [ "$done" -gt 0 ] && [ "$SECONDS" -gt 0 ]; then
    eta=$(( (N-done)*SECONDS/done ))
    printf '\r\033[K  %d/%d (%d%%)  %s elapsed  ETA %s' "$done" "$N" "$pct" "$(fmt_dur "$SECONDS")" "$(fmt_dur "$eta")"
  else
    printf '\r\033[K  %d/%d (%d%%)' "$done" "$N" "$pct"
  fi
done < <("$BIN" "$MODEL" < "$WORK/reqs.ndjson" 2>/dev/null)
printf '\r\033[K  %d/%d done in %s\n' "$ok" "$N" "$(fmt_dur "$SECONDS")"
[ "$fail" -eq 0 ] || echo "  ($fail chunk(s) failed — they are skipped in the output)" >&2

ls "$WORK"/c*.wav >/dev/null 2>&1 || { echo "no audio produced"; exit 1; }
echo "→ concatenating $ok WAVs → $OUT"
for f in "$WORK"/c*.wav; do printf "file '%s'\n" "$f"; done > "$WORK/list.txt"
ffmpeg -hide_banner -loglevel error -f concat -safe 0 -i "$WORK/list.txt" -c copy "$OUT"
echo "✓ $OUT ($(du -h "$OUT" | cut -f1))"
