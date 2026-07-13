#!/usr/bin/env python3
"""Precompute stress-review flag sets from RUAccent's own dictionaries.

RUAccent bundles accents.json (~3.2M word→stress forms) and omographs.json (~20k stress-ambiguous
words). Using them we can flag exactly RUAccent's genuine misses instead of a hand heuristic:
a polysyllabic word left unstressed that is NOT in accents.json is a real skip (a name / rare word
F5 will mis-stress), whereas это/перед/его are unstressed on purpose (in the dict, frequent/proclitic
— F5 says them fine). Scans the STRESSED review files and writes two small sidecars the Go reviewer
loads (so it never touches the 167 MB dict):

  _omographs.txt — book words that are stress-homographs (после/один/руки): a subtle "glance" mark
                   when RUAccent stressed them, a hard flag when it left them unstressed.
  _unknown.txt   — polysyllabic book words absent from accents.json and left unstressed (воровочка,
                   райцеж…): a hard "stress by ear" flag.

  dict_flags.py <accents.json> <omographs.json> <review_dir>
"""
import glob
import json
import os
import re
import sys

VOW = "аеёиоуыэюя"


def trim(s):
    return re.sub(r"^[^а-яё-]+|[^а-яё-]+$", "", s)


def main():
    accents_p, omo_p, review = sys.argv[1], sys.argv[2], sys.argv[3]
    acc = set(json.load(open(accents_p, encoding="utf-8")).keys())
    omo = set(json.load(open(omo_p, encoding="utf-8")).keys())
    omo_book, unk = set(), set()
    for f in glob.glob(os.path.join(review, "[0-9]*_*.txt")):  # the STRESSED output
        if f.endswith(".raw.txt"):
            continue
        for ln in open(f, encoding="utf-8"):
            for tok in ln.split():
                for seg in tok.split("-"):
                    s = trim(seg.lower())
                    base = s.replace("+", "")
                    if not base:
                        continue
                    if base in omo:
                        omo_book.add(base)
                    elif "+" not in s and "ё" not in base and base not in acc \
                            and len(re.findall(f"[{VOW}]", base)) >= 2:
                        unk.add(base)
    open(os.path.join(review, "_omographs.txt"), "w", encoding="utf-8").write("\n".join(sorted(omo_book)) + "\n")
    open(os.path.join(review, "_unknown.txt"), "w", encoding="utf-8").write("\n".join(sorted(unk)) + "\n")
    print(f"  dict flags: {len(omo_book)} omograph words, {len(unk)} unknown (by-ear) words")


if __name__ == "__main__":
    main()
