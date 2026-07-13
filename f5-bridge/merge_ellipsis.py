#!/usr/bin/env python3
"""Re-insert '…' into an already-stressed review file, in place.

RUAccent strips U+2026 '…'. We don't want to re-stress (nondeterministic + would clobber hand
edits), so we surgically graft each '…' back onto the EXISTING stressed text at the right spot,
aligned to the raw source. Alignment is char-level via difflib on normalized strings (drop '+',
fold ё→е) so it survives stress marks, ё-restoration, and homograph letter swaps. '…' always
attaches to a word (no standalone ' … '), so it lands right after that word's last letter.

  merge_ellipsis.py raw.txt stressed.txt   # rewrites stressed.txt (backup .bak), 1:1 lines
"""
import difflib
import sys


def norm(s):
    """normalized chars (drop '+', ё→е) and, per kept char, its original index in s."""
    chars, idx = [], []
    for i, ch in enumerate(s):
        if ch == "+":
            continue
        chars.append("е" if ch == "ё" else ch)
        idx.append(i)
    return "".join(chars), idx


def graft(raw, stressed):
    if "…" not in raw:
        return stressed
    # raw normalized WITHOUT '…', recording after which normalized-index each '…' sits
    rn, ell_after = [], []
    for ch in raw:
        if ch == "…":
            ell_after.append(len(rn) - 1)
            continue
        rn.append("е" if ch == "ё" else ch)
    rn = "".join(rn)
    sn, sidx = norm(stressed)
    sm = difflib.SequenceMatcher(None, rn, sn, autojunk=False)
    r2s = {}
    for a, b, size in sm.get_matching_blocks():
        for k in range(size):
            r2s[a + k] = b + k
    inserts = []
    for anchor in ell_after:
        j = anchor
        while j >= 0 and j not in r2s:  # nearest aligned raw char at/left of the '…'
            j -= 1
        inserts.append(0 if j < 0 else sidx[r2s[j]] + 1)  # insert after that stressed char
    res = stressed
    for spos in sorted(inserts, reverse=True):
        res = res[:spos] + "…" + res[spos:]
    return res


def main():
    raw_p, str_p = sys.argv[1], sys.argv[2]
    raws = open(raw_p, encoding="utf-8").read().split("\n")
    strs = open(str_p, encoding="utf-8").read().split("\n")
    if len(raws) != len(strs):
        sys.exit(f"line mismatch: raw={len(raws)} stressed={len(strs)}")
    out = [graft(r, s) for r, s in zip(raws, strs)]
    open(str_p + ".bak", "w", encoding="utf-8").write("\n".join(strs))
    open(str_p, "w", encoding="utf-8").write("\n".join(out))
    added = sum(o.count("…") - s.count("…") for o, s in zip(out, strs))
    print(f"{str_p}: grafted {added} '…' ({len(raws)} lines, backup .bak)")


if __name__ == "__main__":
    main()
