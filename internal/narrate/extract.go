package narrate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Unit is one narration output: a whole part or a single chapter. ID is "NN" (whole part) or "NN.MM"
// (nested chapter); Disp is the human title (for _titles.tsv); Text is the joined paragraph text.
type Unit struct {
	ID   string
	Disp string
	Text string
}

// part is an intermediate: a top-level part and its ordered chapters (each a []*node of paragraphs).
type part struct {
	title    string
	chapters []chapter
	// paras carried directly by the part (sectioned: part-level <p>; flat: paragraphs before any
	// Раздел heading). Rendered with the part itself / its first chapter.
	intro []*node
}

type chapter struct {
	title string
	paras []*node
}

// Flat-book heading keyword at the start of a bold paragraph. NB: RE2 `\b` is ASCII-only (Cyrillic
// isn't a word char to it), so we bound with `\P{L}` (a non-letter) or end instead.
var flatHeadRe = regexp.MustCompile(`(?i)^(часть|раздел|глава)(\P{L}|$)`)

// buildParts turns the main body into the part→chapter tree, for both book shapes.
func buildParts(mb *node) []part {
	tops := mb.sections()
	// Flat shape: exactly one section with no nested sections but bold heading paragraphs.
	if len(tops) == 1 && len(tops[0].sections()) == 0 {
		if fp := flatParts(tops[0]); fp != nil {
			return fp
		}
	}
	// Sectioned shape.
	var parts []part
	for _, top := range tops {
		p := part{title: top.titleText()}
		for _, c := range top.children {
			if c.skip {
				continue
			}
			switch c.kind {
			case kSection:
				p.chapters = append(p.chapters, chapter{title: c.titleText(), paras: paragraphsOf(c)})
			case kParagraph:
				p.intro = append(p.intro, c)
			}
		}
		parts = append(parts, p)
	}
	return parts
}

// flatParts groups a single flat section's paragraph stream at bold "Часть"/"Раздел" headings.
// Returns nil if no such headings (caller falls back to the whole section as one part).
func flatParts(sec *node) []part {
	var parts []part
	var cur *part
	var curCh *chapter
	sawHead := false
	flush := func() {
		if curCh != nil {
			cur.chapters = append(cur.chapters, *curCh)
			curCh = nil
		}
	}
	for _, c := range sec.children {
		if c.skip {
			continue
		}
		if c.kind == kParagraph && c.bold && flatHeadRe.MatchString(c.text) {
			sawHead = true
			if strings.HasPrefix(strings.ToLower(c.text), "часть") {
				flush()
				parts = append(parts, part{title: c.text})
				cur = &parts[len(parts)-1]
			} else { // Раздел / Глава
				if cur == nil { // Раздел before any Часть — synth a wrapper part
					parts = append(parts, part{title: ""})
					cur = &parts[len(parts)-1]
				}
				flush()
				curCh = &chapter{title: c.text}
			}
			continue
		}
		if c.kind != kParagraph {
			continue
		}
		switch {
		case curCh != nil:
			curCh.paras = append(curCh.paras, c)
		case cur != nil:
			cur.intro = append(cur.intro, c)
		}
	}
	if cur != nil {
		flush()
	}
	if !sawHead {
		return nil
	}
	return parts
}

func paragraphsOf(n *node) []*node {
	var out []*node
	for _, c := range n.children {
		if c.skip {
			continue
		}
		if c.kind == kParagraph {
			out = append(out, c)
		}
		// nested-section paragraphs (a section split deeper than 2 levels) ride along flattened
		if c.kind == kSection {
			out = append(out, paragraphsOf(c)...)
		}
	}
	return out
}

func joinParas(ps []*node, notes map[string]string) string {
	var b strings.Builder
	for _, p := range ps {
		t := resolveNotes(p.text, notes)
		if strings.TrimSpace(t) == "" {
			continue
		}
		b.WriteString(t)
		b.WriteByte('\n')
	}
	return b.String()
}

var markerRe = regexp.MustCompile(string(refOpen) + `([^` + string(refClose) + `]*)` + string(refClose))
var leadingNumRe = regexp.MustCompile(`^\d+\s*`)

// notePrefix is the spoken lead-in for an inlined footnote ("Примечание." ru · "Note." en · "Примітка."
// uk). Set per-invocation by Extract from --note-prefix; extract is a single-shot process (one book,
// no concurrency) so a package var is safe and spares threading the prefix through Units→joinParas.
var notePrefix = "Примечание."

// bracketMode enables the in-body "[N]" footnote convention (see parseBracketNotes): resolveNotes then
// also inlines "[N]" markers whose number is a known note. Off by default so href-marker books and the
// unit tests keep their exact behavior; Extract turns it on only when a "КОММЕНТАРИИ" region was parsed.
var bracketMode bool

var inTextBracketRe = regexp.MustCompile(`\[(\d{1,4})\]`)

// resolveNotes replaces each footnote reference with the note read aloud as "<notePrefix> <text>",
// SEP-bracketed so the chunker isolates it (and the note-mask can route it to a 2nd voice). It handles
// the <a href="#id"> marker convention always, and the plain-text "[N]" convention when bracketMode is
// on. Unknown/empty href refs are dropped; unknown "[N]" is left as literal text (it may not be a note).
func resolveNotes(text string, notes map[string]string) string {
	if strings.ContainsRune(text, refOpen) {
		text = markerRe.ReplaceAllStringFunc(text, func(m string) string {
			id := markerRe.FindStringSubmatch(m)[1]
			n := notes[id]
			if strings.TrimSpace(n) == "" {
				return ""
			}
			return string(SEP) + notePrefix + " " + n + string(SEP)
		})
	}
	if bracketMode && strings.IndexByte(text, '[') >= 0 {
		text = inTextBracketRe.ReplaceAllStringFunc(text, func(m string) string {
			n := notes[inTextBracketRe.FindStringSubmatch(m)[1]]
			if strings.TrimSpace(n) == "" {
				return m
			}
			return string(SEP) + notePrefix + " " + n + string(SEP)
		})
	}
	return text
}

var commentsHeadRe = regexp.MustCompile(`(?i)^(комментари[ий]|примечани[яе]|сноски|примітки|коментар[іи]|notes)$`)
var bracketDefRe = regexp.MustCompile(`^\[(\d{1,4})\]\s*(.+)$`)

// parseBracketNotes handles the in-body footnote convention (e.g. Russian "11/22/63"): a bold
// "КОММЕНТАРИИ"/"ПРИМЕЧАНИЯ" heading followed by "[N] text" definition paragraphs, with plain "[N]"
// markers in the narrative. It returns id→text (id = the number) plus the set of nodes (heading +
// definitions + the blank lines between them) to drop from narration, so the comment list isn't read
// aloud at the end. Returns nil,nil if no such region exists.
func parseBracketNotes(mb *node) (map[string]string, map[*node]bool) {
	var paras []*node
	var walk func(*node)
	walk = func(n *node) {
		for _, c := range n.children {
			switch c.kind {
			case kParagraph:
				paras = append(paras, c)
			case kSection:
				walk(c)
			}
		}
	}
	walk(mb)

	// Look for a bold comments heading actually followed by "[N] …" definitions — so a chapter merely
	// titled "Примечания" (no defs after it) is skipped, and we find the real region (usually at the end).
	for i, head := range paras {
		if !(head.bold && commentsHeadRe.MatchString(strings.TrimSpace(head.text))) {
			continue
		}
		notes := map[string]string{}
		skip := map[*node]bool{head: true}
		for _, p := range paras[i+1:] {
			t := strings.TrimSpace(p.text)
			if t == "" { // empty-line between definitions
				skip[p] = true
				continue
			}
			m := bracketDefRe.FindStringSubmatch(t)
			if m == nil { // first non-"[N] …" paragraph ends the comments region
				break
			}
			notes[m[1]] = m[2]
			skip[p] = true
		}
		if len(notes) > 0 {
			return notes, skip
		}
	}
	return nil, nil
}

// parseNotes builds id→text from the <body name="notes"/"comments"> sections (leading number dropped).
func parseNotes(bodies []*node) map[string]string {
	notes := map[string]string{}
	for _, b := range bodies {
		if b.id != "notes" && b.id != "comments" {
			continue
		}
		for _, sec := range b.sections() {
			if sec.id == "" {
				continue
			}
			txt := leadingNumRe.ReplaceAllString(collapse(allText(sec)), "")
			if txt != "" {
				notes[sec.id] = txt
			}
		}
	}
	return notes
}

// allText concatenates all title + paragraph text under n (recursively).
func allText(n *node) string {
	var b strings.Builder
	var walk func(*node)
	walk = func(x *node) {
		for _, c := range x.children {
			switch c.kind {
			case kTitle, kParagraph:
				if c.text != "" {
					b.WriteString(c.text)
					b.WriteByte(' ')
				}
			case kSection:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}

// Units applies the PARTS selector + COMBINE to the parsed body, producing the ordered output units.
// selector tokens: "P" | "P1-P2" | "P:S" | "P:S1-S2" (space-separated); empty ⇒ all parts.
// combine: 1 ⇒ one unit per whole part; 2 ⇒ one unit per chapter (for whole-part selections).
func Units(mb *node, selector string, combine int, notes map[string]string) []Unit {
	parts := buildParts(mb)
	if combine == 0 {
		combine = 1
	}
	var out []Unit
	emitPart := func(pi int) { // pi is 1-based
		if pi < 1 || pi > len(parts) {
			return
		}
		p := parts[pi-1]
		if combine == 2 && len(p.chapters) > 0 {
			for ci := range p.chapters {
				out = append(out, chapterUnit(p, pi, ci+1, notes))
			}
			return
		}
		out = append(out, wholePartUnit(p, pi, notes))
	}
	emitChapter := func(pi, ci int) {
		if pi < 1 || pi > len(parts) {
			return
		}
		p := parts[pi-1]
		if ci < 1 || ci > len(p.chapters) {
			return
		}
		out = append(out, chapterUnit(p, pi, ci, notes))
	}

	toks := strings.Fields(selector)
	if len(toks) == 0 {
		for pi := range parts {
			emitPart(pi + 1)
		}
		return out
	}
	for _, tok := range toks {
		if p, sr, ok := strings.Cut(tok, ":"); ok {
			pi := atoi(p)
			for _, s := range expandRange(sr) {
				emitChapter(pi, s)
			}
		} else {
			for _, pi := range expandRange(tok) {
				emitPart(pi)
			}
		}
	}
	return out
}

func writeHeading(b *strings.Builder, h string) {
	if h != "" {
		b.WriteString(h)
		b.WriteByte('\n')
	}
}

func wholePartUnit(p part, pi int, notes map[string]string) Unit {
	var b strings.Builder
	if len(p.chapters) == 0 {
		writeHeading(&b, spokenHeading(p.title, "", true)) // announce the part title
		b.WriteString(joinParas(p.intro, notes))
	} else {
		for ci, ch := range p.chapters { // each chapter announced within the part
			writeHeading(&b, spokenHeading(p.title, ch.title, ci == 0))
			if ci == 0 {
				b.WriteString(joinParas(p.intro, notes))
			}
			b.WriteString(joinParas(ch.paras, notes))
		}
	}
	disp := p.title
	if disp == "" {
		disp = fmt.Sprintf("part_%d", pi)
	}
	return Unit{ID: fmt.Sprintf("%02d", pi), Disp: disp, Text: b.String()}
}

func chapterUnit(p part, pi, ci int, notes map[string]string) Unit {
	ch := p.chapters[ci-1]
	var b strings.Builder
	first := ci == 1
	writeHeading(&b, spokenHeading(p.title, ch.title, first))
	if first {
		b.WriteString(joinParas(p.intro, notes)) // part intro rides with its first chapter
	}
	b.WriteString(joinParas(ch.paras, notes))
	disp := ch.title
	if disp == "" {
		disp = fmt.Sprintf("part_%d_%d", pi, ci)
	}
	return Unit{ID: fmt.Sprintf("%02d.%02d", pi, ci), Disp: disp, Text: b.String()}
}

// expandRange turns "3" → [3] and "2-5" → [2,3,4,5]; junk → nil.
func expandRange(s string) []int {
	if a, b, ok := strings.Cut(s, "-"); ok {
		lo, hi := atoi(a), atoi(b)
		if lo == 0 || hi == 0 || hi < lo {
			return nil
		}
		var out []int
		for i := lo; i <= hi; i++ {
			out = append(out, i)
		}
		return out
	}
	if n := atoi(s); n > 0 {
		return []int{n}
	}
	return nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

var sentSplitRe = regexp.MustCompile(`([.!?]["»)]*)\s+`)
var safeStripRe = regexp.MustCompile(`[^A-Za-z0-9_А-Яа-яЁёІіЇїЄєҐґ.-]`)

// Chunk splits at SEP boundaries (so each inlined footnote is chunked on its own, never packed into
// the surrounding narrative), then within each segment collapses whitespace, sentence-splits, and
// greedily packs sentences into lines ≤ maxchars — matching the old `sed | gawk` chunker's sizes.
func Chunk(text string, maxchars int) []string {
	chunks, _ := ChunkMask(text, maxchars)
	return chunks
}

// ChunkMask is Chunk plus a per-chunk note flag: true when the chunk came from an inlined footnote.
// resolveNotes always emits a note as a balanced SEP…SEP pair, so splitting the unit text on SEP puts
// narration on even segment indices and notes on odd ones — every chunk of an odd segment is a note.
// The mask lets the synth route footnote chunks to a second voice (F5MODEL_NOTES).
func ChunkMask(text string, maxchars int) (chunks []string, notes []bool) {
	for i, seg := range strings.Split(text, string(SEP)) {
		isNote := i%2 == 1
		for _, c := range chunkSegment(seg, maxchars) {
			chunks = append(chunks, c)
			notes = append(notes, isNote)
		}
	}
	return chunks, notes
}

func chunkSegment(text string, maxchars int) []string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = collapse(text)
	if text == "" {
		return nil
	}
	sents := sentSplitRe.ReplaceAllString(text, "$1\n")
	var out []string
	var buf string
	for _, s := range strings.Split(sents, "\n") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		switch {
		case buf == "":
			buf = s
		case utf8.RuneCountInString(buf)+1+utf8.RuneCountInString(s) <= maxchars: // chars, like gawk length()
			buf += " " + s
		default:
			out = append(out, buf)
			buf = s
		}
		// A sentence ending in ? or ! ends its chunk, so the question/exclamation lands at the end of
		// the utterance — F5 renders its intonation there, but flattens it mid-chunk. The chunk keeps
		// whatever narrative packed before it, so it isn't a tiny clip-prone fragment.
		if endsQuestionOrBang(s) {
			out = append(out, buf)
			buf = ""
		}
	}
	if buf != "" {
		out = append(out, buf)
	}
	return out
}

// endsQuestionOrBang reports whether s ends with ? or ! (ignoring trailing quotes/brackets).
func endsQuestionOrBang(s string) bool {
	s = strings.TrimRight(s, `"»)'’” `)
	return strings.HasSuffix(s, "?") || strings.HasSuffix(s, "!")
}

// maskBits renders a note-mask as a '0'/'1' byte string, or nil if no chunk is a note (so the sidecar
// is skipped entirely for note-less units).
func maskBits(mask []bool) []byte {
	any := false
	bits := make([]byte, len(mask))
	for i, b := range mask {
		if b {
			bits[i] = '1'
			any = true
		} else {
			bits[i] = '0'
		}
	}
	if !any {
		return nil
	}
	return bits
}

func safeName(disp, fallback string) string {
	s := strings.ReplaceAll(disp, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = safeStripRe.ReplaceAllString(s, "")
	if s == "" {
		return fallback
	}
	return s
}

// Extract parses fb2Path and writes, into reviewDir, one `<ID>_<safe>.raw.txt` (chunked) per selected
// unit plus a `_titles.tsv` manifest (ID<TAB>safe<TAB>display) — the drop-in the F5 pipeline reads.
// A `<ID>_<safe>.notes` bitstring sidecar (one 0/1 per chunk line) is written when the unit has any
// inlined footnote, so ndjson-reqs can route note chunks to the notes voice. notePfx overrides the
// spoken footnote lead-in (empty ⇒ default "Примечание."). Returns the section-structure map lines.
func Extract(fb2Path, reviewDir string, maxchars int, selector string, combine int, notePfx string) ([]string, error) {
	if notePfx != "" {
		notePrefix = notePfx
	}
	fh, err := os.Open(fb2Path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	bodies, err := Parse(fh)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", fb2Path, err)
	}
	mb := MainBody(bodies)
	if mb == nil {
		return nil, fmt.Errorf("no main body in %s", fb2Path)
	}
	// Footnotes: the <body name="notes"> href convention, else the in-body "[N]"/КОММЕНТАРИИ convention
	// (drop its comment list from narration and inline the [N] markers instead).
	notes := parseNotes(bodies)
	if len(notes) == 0 {
		if bnotes, skip := parseBracketNotes(mb); len(bnotes) > 0 {
			notes = bnotes
			bracketMode = true
			for n := range skip {
				n.skip = true
			}
		}
	}
	parts := buildParts(mb)
	var mapLines []string
	mapLines = append(mapLines, fmt.Sprintf("%d part(s):", len(parts)))
	for i, p := range parts {
		t := p.title
		if t == "" {
			t = "(no title)"
		}
		mapLines = append(mapLines, fmt.Sprintf("   [%d] %s → %d chapters", i+1, t, len(p.chapters)))
	}

	units := Units(mb, selector, combine, notes)
	if err := os.MkdirAll(reviewDir, 0o755); err != nil {
		return mapLines, err
	}
	tsv, err := os.Create(filepath.Join(reviewDir, "_titles.tsv"))
	if err != nil {
		return mapLines, err
	}
	defer tsv.Close()
	for _, u := range units {
		safe := safeName(u.Disp, "part_"+u.ID)
		chunks, mask := ChunkMask(u.Text, maxchars)
		raw := filepath.Join(reviewDir, fmt.Sprintf("%s_%s.raw.txt", u.ID, safe))
		if err := os.WriteFile(raw, []byte(strings.Join(chunks, "\n")+"\n"), 0o644); err != nil {
			return mapLines, err
		}
		// note-mask sidecar: one '0'/'1' per chunk, aligned with the (line-preserving) stressed .txt.
		// Only written when the unit has ≥1 footnote; ndjson-reqs treats an absent file as all-narration.
		if bits := maskBits(mask); bits != nil {
			nf := filepath.Join(reviewDir, fmt.Sprintf("%s_%s.notes", u.ID, safe))
			if err := os.WriteFile(nf, append(bits, '\n'), 0o644); err != nil {
				return mapLines, err
			}
		}
		if _, err := fmt.Fprintf(tsv, "%s\t%s\t%s\n", u.ID, safe, u.Disp); err != nil {
			return mapLines, err
		}
	}
	return mapLines, nil
}
