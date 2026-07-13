#!/usr/bin/env python3
"""FB2 → per-chapter narration text for the F5 pipeline (replaces the old xmllint/awk extract).

Splits the main body at the SECOND level (<section>/<section> = chapters); a part with no
sub-sections stays one unit. Each unit gets a spoken heading: the first chapter of a part is
prefixed with the part title, the rest just "Глава <ordinal>." (numeric chapter titles are voiced
as feminine ordinals — F5 would read a bare digit as a cardinal). Footnotes from <body name="notes">
are read INLINE at the end of the sentence that references them, as 'Примечание. <text>' (the note's
own leading number is dropped). Output: one chunked <=MAXCHARS line per row in NN_<safe>.raw.txt +
a _titles.tsv manifest (NN\tsafe\tdisplay-title) — the shell then runs RUAccent per file.

  fb2_extract.py <book.fb2> <review_dir> <maxchars> [parts_csv]
"""
import os
import re
import sys
import xml.etree.ElementTree as ET

NOTE_OPEN, NOTE_CLOSE = "", ""  # private-use markers for a footnote ref in the text

# Feminine ordinals (глава f.) 1..40; compose 21+ from tens + unit. Fallback = the digit itself.
_ORD = {1:"первая",2:"вторая",3:"третья",4:"четвёртая",5:"пятая",6:"шестая",7:"седьмая",
        8:"восьмая",9:"девятая",10:"десятая",11:"одиннадцатая",12:"двенадцатая",13:"тринадцатая",
        14:"четырнадцатая",15:"пятнадцатая",16:"шестнадцатая",17:"семнадцатая",18:"восемнадцатая",
        19:"девятнадцатая",20:"двадцатая",30:"тридцатая",40:"сороковая"}
_TENS = {20:"двадцать",30:"тридцать",40:"сорок"}


def ordinal_fem(n):
    if n in _ORD:
        return _ORD[n]
    if 20 < n < 40 and n % 10:
        return f"{_TENS[(n // 10) * 10]} {_ORD[n % 10]}"
    return str(n)


def local(tag):
    return tag.rsplit("}", 1)[-1]


def title_text(el):
    """flat text of a <title>/<section>-title element, spaces collapsed."""
    return re.sub(r"\s+", " ", "".join(el.itertext())).strip()


def note_id(a):
    for k, v in a.attrib.items():
        if k.endswith("href"):
            return v.lstrip("#")
    return None


def collect(elems, out):
    """append text of `elems` to list `out`, emitting NOTE markers for <a type=note> refs."""
    def walk(e):
        if local(e.tag) == "a" and e.get("type") == "note":
            nid = note_id(e)
            if nid:
                out.append(f"{NOTE_OPEN}{nid}{NOTE_CLOSE}")
            if e.tail:
                out.append(e.tail)
            return
        if local(e.tag) == "title":  # own heading is synthesized separately — skip the raw title
            if e.tail:
                out.append(e.tail)
            return
        if e.text:
            out.append(e.text)
        for c in e:
            walk(c)
        if e.tail:
            out.append(e.tail)
    for el in elems:
        walk(el)


def inline_notes(text, notes):
    """move each NOTE marker to the end of its sentence as 'Примечание. <note text>'."""
    rx = re.compile(f"{NOTE_OPEN}([^{NOTE_CLOSE}]+){NOTE_CLOSE}")
    while True:
        m = rx.search(text)
        if not m:
            break
        note = notes.get(m.group(1), "")
        ins = f" Примечание. {note} " if note else " "
        before, after = text[: m.start()], text[m.end():]
        if re.search(r"[.!?…][\"»)\]]*\s*$", before):  # marker sat right after a sentence end
            text = before + ins + after
        else:
            se = re.search(r"[.!?…][\"»)\]]*", after)  # else attach after the next sentence end
            cut = se.end() if se else len(after)
            text = before + after[:cut] + ins + after[cut:]
    return text


def chunk(text, maxchars):
    """collapse ws → sentence-split (after . ! ? + closing quotes) → greedy pack to <=maxchars."""
    text = re.sub(r"\s+", " ", text).strip()
    sents = re.sub(r'([.!?]["»)]*)\s+', r"\1\n", text).split("\n")
    out, buf = [], ""
    for s in sents:
        s = s.strip()
        if not s:
            continue
        if not buf:
            buf = s
        elif len(buf) + 1 + len(s) <= maxchars:
            buf += " " + s
        else:
            out.append(buf)
            buf = s
    if buf:
        out.append(buf)
    return out


def safe_name(s, fallback):
    s = (s or "").strip() or fallback
    s = s.replace(" ", "_").replace("/", "_")
    s = re.sub(r"[^A-Za-z0-9_А-Яа-яЁёІіЇїЄєҐґ.-]", "", s)
    return s[:40] or fallback


def main():
    fb2, review, maxchars = sys.argv[1], sys.argv[2], int(sys.argv[3])
    parts_filter = set(int(x) for x in sys.argv[4].split()) if len(sys.argv) > 4 and sys.argv[4].strip() else None
    root = ET.parse(fb2).getroot()
    bodies = [b for b in root.iter() if local(b.tag) == "body"]
    main_body = next((b for b in bodies if not b.get("name")), None)
    notes_body = next((b for b in bodies if b.get("name") in ("notes", "comments")), None)

    notes = {}
    if notes_body is not None:
        for sec in notes_body:
            if local(sec.tag) != "section":
                continue
            nid = sec.get("id")
            if not nid:
                continue
            txt = re.sub(r"\s+", " ", "".join(sec.itertext())).strip()
            notes[nid] = re.sub(r"^\d+\s*", "", txt)  # drop the note's own leading number

    parts = [s for s in main_body if local(s.tag) == "section"]
    seq = 0  # global chapter counter — stays stable even when parts_filter drops earlier parts
    titles = []
    for pi, part in enumerate(parts, 1):
        ptitle_el = next((c for c in part if local(c.tag) == "title"), None)
        ptitle = title_text(ptitle_el) if ptitle_el is not None else ""
        chapters = [c for c in part if local(c.tag) == "section"]
        # part-level content sitting outside any chapter (epigraph etc.) rides with the first chapter
        pintro = [c for c in part if local(c.tag) not in ("section", "title")]

        units = chapters if chapters else [part]
        for ci, chap in enumerate(units):
            seq += 1
            if parts_filter and pi not in parts_filter:
                continue  # counted (keeps numbering global) but not written
            first = ci == 0
            if chapters:
                tel = next((c for c in chap if local(c.tag) == "title"), None)
                ct = title_text(tel) if tel is not None else ""
                chead = f"Глава {ordinal_fem(int(ct))}." if re.fullmatch(r"\d+", ct) else (ct + "." if ct else "")
                heading = (ptitle + ". " if first and ptitle else "") + chead
                disp = f"{ptitle} — {ct}" if first and ptitle else ct
                sname = safe_name((ptitle + "_" + ct) if first else ct, f"ch{seq}")
                bodyels = (pintro if first else []) + [c for c in chap if local(c.tag) != "title"]
            else:  # leaf part (no chapters) — the whole part is one unit
                heading = (ptitle + "." if ptitle else "")
                disp, sname = ptitle, safe_name(ptitle, f"part{pi}")
                bodyels = pintro + [c for c in part if local(c.tag) not in ("title",)]

            acc = []
            collect(bodyels, acc)
            body = inline_notes("".join(acc), notes)
            lines = ([heading] if heading else []) + chunk(body, maxchars)
            nn = f"{seq:02d}"
            with open(os.path.join(review, f"{nn}_{sname}.raw.txt"), "w", encoding="utf-8") as f:
                f.write("\n".join(lines) + "\n")
            titles.append((nn, sname, disp))

    with open(os.path.join(review, "_titles.tsv"), "w", encoding="utf-8") as f:
        for nn, sname, disp in titles:
            f.write(f"{nn}\t{sname}\t{disp}\n")


if __name__ == "__main__":
    main()
