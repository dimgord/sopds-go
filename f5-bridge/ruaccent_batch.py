#!/usr/bin/env python3
"""Batch RUAccent stress placement: text chunks (one per line) in -> stressed out.

Robust: a chunk whose stressing throws falls back to the unstressed text (logged to
stderr) so it still gets synthesized. `--tiny` uses the dictionary-only path (no neural
omograph model — avoids its token_type_ids bug, loses homograph disambiguation).
"""
import argparse
import json
import os
import sys


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tiny", action="store_true", help="dictionary-only (skip omograph model)")
    ap.add_argument("--fix", help='corrections JSON: {"yo":{word:form}, "replace":{from:to}}')
    ap.add_argument("--dump-homographs", help="write the ambiguous yo/stress homograph words here and exit")
    args = ap.parse_args()

    from ruaccent import RUAccent
    acc = RUAccent()
    acc.load(omograph_model_size="turbo2", use_dictionary=True, tiny_mode=args.tiny)

    if args.dump_homographs:
        words = set(getattr(acc, "yo_homographs", {})) | set(getattr(acc, "omographs", {}))
        with open(args.dump_homographs, "w", encoding="utf-8") as f:
            f.write("\n".join(sorted(words)))
        return

    # Per-word fixes for RUAccent's residual homograph/ё errors (e.g. берет→берёт).
    # "yo" overrides the ё-homograph dictionary; "replace" is a blunt final substitution.
    replace = {}
    if args.fix and os.path.exists(args.fix):
        fx = json.load(open(args.fix, encoding="utf-8"))
        acc.yo_homographs.update(fx.get("yo", {}))
        replace = fx.get("replace", {})
    sys.stderr.write(f"ruaccent ready (tiny={args.tiny} fixes={len(replace)}+{len(getattr(acc,'yo_homographs',{})) and 'yo'})\n")

    fails = 0
    for line in sys.stdin:
        t = line.rstrip("\n")
        if not t.strip():
            sys.stdout.write(t + "\n")
            continue
        try:
            # RUAccent drops the ellipsis char '…' but keeps '...'; swap so it survives, then
            # restore '…' (F5's trained token, id 1844) — preserves the book's ~560 pauses.
            s = acc.process_all(t.replace("…", "..."))
            s = s.replace("...", "…")
            for a, b in replace.items():
                s = s.replace(a, b)
            sys.stdout.write(s + "\n")
        except Exception as e:
            fails += 1
            sys.stderr.write(f"stress-fallback: {e}\n")
            sys.stdout.write(t + "\n")
        sys.stdout.flush()
    sys.stderr.write(f"ruaccent done: {fails} fallback(s)\n")


if __name__ == "__main__":
    main()
