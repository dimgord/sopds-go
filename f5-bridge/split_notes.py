#!/usr/bin/env python3
"""Isolate footnotes in an already-stressed review (option B): split every chunk that contains a
"Примечание. <text>" into up to three chunks — narrative-before / the note / narrative-after — so
each note becomes its own chunk (for a separate voice + pauses at synth). Works in place on the
review .txt (stressed) AND .raw.txt, keeping them 1:1 line-aligned and preserving all stress edits
(no RUAccent re-run). The note span is found EXACTLY in the raw text (it equals notes[id]); the same
span in the stressed line is located by difflib char-alignment (tolerant of '+', ё, homograph swaps).

  split_notes.py <book.fb2> <review_dir>
"""
import difflib
import glob
import os
import re
import sys
import xml.etree.ElementTree as ET


def local(tag):
    return tag.rsplit("}", 1)[-1]


def load_notes(fb2):
    root = ET.parse(fb2).getroot()
    nb = next((b for b in root.iter() if local(b.tag) == "body" and b.get("name") in ("notes", "comments")), None)
    notes = []
    if nb is not None:
        for sec in nb:
            if local(sec.tag) == "section" and sec.get("id"):
                t = re.sub(r"\s+", " ", "".join(sec.itertext())).strip()
                notes.append(re.sub(r"^\d+\s*", "", t))
    return sorted(notes, key=len, reverse=True)  # longest first, for greedy matching


def norm(s):
    """lowercase, fold ё→е, drop '+'; return (normalized, orig-index-per-normalized-char)."""
    out, idx = [], []
    for i, c in enumerate(s):
        if c == "+":
            continue
        out.append("е" if c in "ёЁ" else c.lower())
        idx.append(i)
    return "".join(out), idx


def split_line(raw, stressed, note_texts):
    # find every "Примечание. <note>" span in raw (exact — raw holds notes[id] verbatim)
    spans = []
    pos = 0
    while True:
        m = re.search(r"Примечание\.\s*", raw[pos:])
        if not m:
            break
        s, after = pos + m.start(), pos + m.end()
        best = next((nt for nt in note_texts if raw[after:after + len(nt)] == nt), None)
        if best is None:  # fallback: note ends at the next sentence terminator
            se = re.search(r'[.!?…]["»)\]]*', raw[after:])
            end = after + (se.end() if se else len(raw) - after)
        else:
            end = after + len(best)
        spans.append((s, end))
        pos = end
    if not spans:
        return None

    rn, _ = norm(raw)              # raw has no '+', so rn index == raw index
    sn, si = norm(stressed)
    r2s = {}
    for a, b, size in difflib.SequenceMatcher(None, rn, sn, autojunk=False).get_matching_blocks():
        for k in range(size):
            r2s[a + k] = b + k

    def to_str(p):                 # raw char pos → stressed char pos
        j = min(p, len(rn))
        while j < len(rn) and j not in r2s:
            j += 1
        return si[r2s[j]] if j in r2s else len(stressed)

    cuts = [0] + [c for s, e in spans for c in (s, e)] + [len(raw)]
    rp = [raw[cuts[i]:cuts[i + 1]].strip() for i in range(len(cuts) - 1)]
    scuts = [0] + [to_str(c) for s, e in spans for c in (s, e)] + [len(stressed)]
    sp = [stressed[scuts[i]:scuts[i + 1]].strip() for i in range(len(scuts) - 1)]
    pairs = [(r, s) for r, s in zip(rp, sp) if r or s]   # drop empty pieces (both sides in lockstep)
    return pairs if len(pairs) > 1 else None


def main():
    fb2, review = sys.argv[1], sys.argv[2]
    note_texts = load_notes(fb2)
    for txt in sorted(glob.glob(os.path.join(review, "[0-9]*_*.txt"))):
        if txt.endswith(".raw.txt"):
            continue
        raw = txt[:-4] + ".raw.txt"
        if not os.path.exists(raw):
            continue
        rlines = open(raw, encoding="utf-8").read().split("\n")
        slines = open(txt, encoding="utf-8").read().split("\n")
        if len(rlines) != len(slines):
            print(f"  ! {os.path.basename(txt)}: line mismatch {len(rlines)}/{len(slines)} — skipped")
            continue
        nr, ns, split = [], [], 0
        for r, s in zip(rlines, slines):
            pairs = split_line(r, s, note_texts) if "Примечание." in r else None
            if pairs:
                split += 1
                nr.extend(p[0] for p in pairs)
                ns.extend(p[1] for p in pairs)
            else:
                nr.append(r)
                ns.append(s)
        if split:
            open(raw, "w", encoding="utf-8").write("\n".join(nr) + "\n")
            open(txt, "w", encoding="utf-8").write("\n".join(ns) + "\n")
        print(f"  {os.path.basename(txt)}: split {split} note line(s), {len(rlines)}→{len(nr)} lines")


if __name__ == "__main__":
    main()
