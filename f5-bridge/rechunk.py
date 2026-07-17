#!/usr/bin/env python3
"""Re-chunk an already-stressed review for F5. Split at EVERY punctuation boundary (. ! ? … ; : ,
and the spaced em-dash) and greedily pack units up to <maxchars>. The synth step now adds a
deterministic pause AFTER each chunk keyed to its trailing punctuation (see fb2-to-f5.sh PUNCT_PAUSE),
so we can split at commas freely — the pause length, not the split point, controls prosody. Raw and
txt are split at the SAME boundaries (identical punctuation) so they stay 1:1 aligned; stress is
preserved. Footnote chunks (start "Примечание.") are kept whole so voice routing isn't broken. The
merge pass still folds a chunk that starts with a stressed vowel (F5 drops it) or a tiny chunk into
the previous one.

  rechunk.py <review_dir> [maxchars=220]
"""
import glob
import os
import re
import sys

SEP = "\x00"
_ACC = re.compile("([аеёиоуыэюяАЕЁИОУЫЭЮЯ])́")  # a stray combining acute → normalize to '+vowel'


def acc_to_plus(s):
    return _ACC.sub(r"+\1", s)


def split_units(s):
    # split after . ! ? … ; : ,  (keep the mark + any closing quote on the left piece)
    s = re.sub(r'([.!?…;:,]["»)”“„’]*)\s+', r"\1" + SEP, s)
    # split after a spaced em-dash, dash kept on the LEFT piece (chunk ends in "—" → em-dash pause)
    s = re.sub(r"\s+(—)\s+", r" \1" + SEP, s)
    return [x.strip() for x in s.split(SEP) if x.strip()]


def pack(units, maxc):
    """greedy-group unit indices to <=maxc chars (by unit length); returns list of index-lists."""
    groups, cur, cl = [], [], 0
    for i, u in enumerate(units):
        if cur and cl + 1 + len(u) > maxc:
            groups.append(cur); cur, cl = [], 0
        cur.append(i); cl += (1 if cl else 0) + len(u)
    if cur:
        groups.append(cur)
    return groups


def regroup(raw, txt, maxc):
    if re.match(r"\s*Примеч", raw):
        return [(raw, txt)]
    ru, tu = split_units(raw), split_units(txt)
    if len(ru) != len(tu):
        return [(raw, txt)]
    return [(" ".join(ru[j] for j in g), " ".join(tu[j] for j in g)) for g in pack(ru, maxc)]


def main():
    review = sys.argv[1]
    maxc = int(sys.argv[2]) if len(sys.argv) > 2 else 220
    for txt in sorted(glob.glob(os.path.join(review, "[0-9]*_*.txt"))):
        if txt.endswith(".raw.txt"):
            continue
        raw = txt[:-4] + ".raw.txt"
        if not os.path.exists(raw):
            continue
        R = [l for l in open(raw, encoding="utf-8").read().split("\n") if l.strip()]
        T = [acc_to_plus(l) for l in open(txt, encoding="utf-8").read().split("\n") if l.strip()]
        if len(R) != len(T):
            print(f"  ! {os.path.basename(txt)}: mismatch {len(R)}/{len(T)} — skipped"); continue
        nr, nt = [], []
        for r, t in zip(R, T):
            for rr, tt in regroup(r, t, maxc):
                nr.append(rr); nt.append(tt)
        # F5 drops a stressed vowel at the very START of a generation ("+Оба" → "ба"), and a tiny
        # chunk generates unstably — merge such a chunk into the previous one (unless either is a
        # footnote, which must stay its own chunk). Keeps them under ~1.6×maxc.
        mr, mt = [], []
        for r, t in zip(nr, nt):
            note = re.match(r"\s*Примеч", t)
            prev_note = mt and re.match(r"\s*Примеч", mt[-1])
            weak = re.match(r"\s*\+[АЕЁИОУЫЭЮЯаеёиоуыэюя]", t) or len(t.replace("+", "").strip()) < 40
            if mr and not note and not prev_note and weak and len((mt[-1] + " " + t).replace("+", "")) <= maxc * 1.25:
                mr[-1] += " " + r; mt[-1] += " " + t
            else:
                mr.append(r); mt.append(t)
        nr, nt = mr, mt
        open(raw, "w", encoding="utf-8").write("\n".join(nr) + "\n")
        open(txt, "w", encoding="utf-8").write("\n".join(nt) + "\n")
        print(f"  {os.path.basename(txt)}: {len(R)} → {len(nr)} chunks")


if __name__ == "__main__":
    main()
