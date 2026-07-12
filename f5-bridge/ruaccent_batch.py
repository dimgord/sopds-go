#!/usr/bin/env python3
"""Batch RUAccent stress placement: text chunks (one per line) in -> stressed out.

Robust: a chunk whose stressing throws falls back to the unstressed text (logged to
stderr) so it still gets synthesized. `--tiny` uses the dictionary-only path (no neural
omograph model — avoids its token_type_ids bug, loses homograph disambiguation).
"""
import argparse
import sys


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tiny", action="store_true", help="dictionary-only (skip omograph model)")
    args = ap.parse_args()

    from ruaccent import RUAccent
    acc = RUAccent()
    acc.load(omograph_model_size="turbo2", use_dictionary=True, tiny_mode=args.tiny)
    sys.stderr.write(f"ruaccent ready (tiny={args.tiny})\n")

    fails = 0
    for line in sys.stdin:
        t = line.rstrip("\n")
        if not t.strip():
            sys.stdout.write(t + "\n")
            continue
        try:
            sys.stdout.write(acc.process_all(t) + "\n")
        except Exception as e:
            fails += 1
            sys.stderr.write(f"stress-fallback: {e}\n")
            sys.stdout.write(t + "\n")
        sys.stdout.flush()
    sys.stderr.write(f"ruaccent done: {fails} fallback(s)\n")


if __name__ == "__main__":
    main()
