#!/usr/bin/env python3
"""Resident F5-TTS daemon — load the model ONCE, synthesize many chunks.

Protocol mirrors sopds-tts-rs: NDJSON requests {"text","output"} on stdin, one NDJSON
response {"ok",...} per line on stdout; `ready: ...` on stderr once loaded. Text is taken
as-is (stress it upstream with ruaccent_batch.py).

BRIDGE ONLY — the real target is a native Rust (ort) F5 port, see
docs/decisions/001-f5-tts-onnx-integration.md.
"""
import argparse
import json
import os
import sys
import time

# The F5/vocos pipeline prints chatter straight to stdout, which would corrupt our NDJSON
# protocol. Keep the real stdout for responses only; send everything else to stderr.
_resp = sys.stdout
sys.stdout = sys.stderr


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--ckpt", required=True)
    ap.add_argument("--vocab", required=True)
    ap.add_argument("--ref", required=True)
    ap.add_argument("--ref-text", required=True, help="file path or literal text")
    ap.add_argument("--model", default="F5TTS_v1_Base")
    ap.add_argument("--nfe", type=int, default=16)
    ap.add_argument("--device", default="cpu")
    ap.add_argument("--remove-silence", action="store_true")
    args = ap.parse_args()

    ref_text = args.ref_text
    if os.path.exists(ref_text):
        with open(ref_text, encoding="utf-8") as f:
            ref_text = f.read().strip()

    from f5_tts.api import F5TTS
    model = F5TTS(model=args.model, ckpt_file=args.ckpt, vocab_file=args.vocab, device=args.device)

    quiet = lambda *a, **k: None  # noqa: E731
    sys.stderr.write(f"ready: F5 loaded (nfe={args.nfe} device={args.device})\n")
    sys.stderr.flush()

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        t0 = time.time()
        try:
            req = json.loads(line)
            model.infer(
                ref_file=args.ref, ref_text=ref_text, gen_text=req["text"],
                nfe_step=args.nfe, file_wave=req["output"], remove_silence=args.remove_silence,
                show_info=quiet,
            )
            resp = {"ok": True, "output": req["output"], "elapsed_ms": int((time.time() - t0) * 1000)}
        except Exception as e:  # one bad chunk must not kill the daemon
            resp = {"ok": False, "error": str(e)}
        _resp.write(json.dumps(resp, ensure_ascii=False) + "\n")
        _resp.flush()


if __name__ == "__main__":
    main()
