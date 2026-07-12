#!/usr/bin/env bash
# Container entrypoint: fetch inputs → narrate the book on the GPU → publish the MP3s.
#
# Required env:
#   BOOK_URL        curl-able .fb2
#   REF_URL         curl-able voice reference (wav/mp3, ~8 s mono)
#   REF_TEXT        exact transcript of the reference  (or REF_TEXT_URL)
# Optional:
#   NFE=16  WORKERS=2  MAXCHARS=250  PARTS=""(all)   DEVICE=cuda
#   OUTPUT_PUT_URL  presigned PUT — the whole out/ is tar.gz'd and uploaded here
#   RCLONE_REMOTE + RCLONE_CONF_URL   alternative: rclone copy out/ to a remote
#   IDLE_AFTER=1    if set, sleep after finishing so you can exec in and copy manually
set -euo pipefail
cd /app

: "${BOOK_URL:?set BOOK_URL}"; : "${REF_URL:?set REF_URL}"
NFE=${NFE:-16}; WORKERS=${WORKERS:-2}; MAXCHARS=${MAXCHARS:-250}; DEVICE=${DEVICE:-cuda}; PARTS=${PARTS:-}

mkdir -p ab out
echo "→ fetching inputs"
curl -fsSL "$BOOK_URL" -o book.fb2
curl -fsSL "$REF_URL"  -o ab/ref_fixed.wav
if [ -n "${REF_TEXT_URL:-}" ]; then curl -fsSL "$REF_TEXT_URL" -o ab/ref_fixed.txt
else printf '%s' "${REF_TEXT:?set REF_TEXT or REF_TEXT_URL}" > ab/ref_fixed.txt; fi

echo "→ GPU check"; /app/f5env/bin/python -c "import torch;print('cuda',torch.cuda.is_available(),torch.cuda.get_device_name(0) if torch.cuda.is_available() else '')"

echo "→ narrating (nfe=$NFE workers=$WORKERS device=$DEVICE)"
F5_HOME=/app NFE="$NFE" WORKERS="$WORKERS" MAXCHARS="$MAXCHARS" DEVICE="$DEVICE" PARTS="$PARTS" \
  /app/f5-bridge/fb2-to-f5.sh /app/book.fb2 /app/out

echo "→ output:"; ls -la /app/out

# Publish
if [ -n "${OUTPUT_PUT_URL:-}" ]; then
  tar -czf /app/audiobook.tar.gz -C /app/out .
  echo "→ uploading audiobook.tar.gz ($(du -h /app/audiobook.tar.gz | cut -f1))"
  curl -fsSL -X PUT --upload-file /app/audiobook.tar.gz "$OUTPUT_PUT_URL"
  echo "✓ uploaded"
elif [ -n "${RCLONE_REMOTE:-}" ] && [ -n "${RCLONE_CONF_URL:-}" ]; then
  curl -fsSL "$RCLONE_CONF_URL" -o /root/rclone.conf
  /app/f5env/bin/pip install --quiet rclone 2>/dev/null || true
  curl -fsSL https://rclone.org/install.sh | bash >/dev/null 2>&1 || true
  rclone --config /root/rclone.conf copy /app/out "$RCLONE_REMOTE" -P
  echo "✓ rclone done"
else
  echo "⚠ no OUTPUT_PUT_URL / RCLONE_REMOTE — output stays in /app/out"
fi

[ -n "${IDLE_AFTER:-}" ] && { echo "idling (exec in to copy /app/out)…"; sleep infinity; }
echo "✓ done"
