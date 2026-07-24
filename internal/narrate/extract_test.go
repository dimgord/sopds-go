package narrate

import (
	"os"
	"strings"
	"testing"
)

func unitIDs(us []Unit) string {
	var ids []string
	for _, u := range us {
		ids = append(ids, u.ID)
	}
	return strings.Join(ids, " ")
}

func TestUnitsFlat(t *testing.T) {
	bodies, _ := Parse(strings.NewReader(flatFB2))
	mb := MainBody(bodies)
	// Structure: Часть 1 {Раздел 1, Раздел 2}, Часть 2 {Раздел 3}.
	if got := unitIDs(Units(mb, "", 1, nil)); got != "01 02" {
		t.Errorf("all/combine1 = %q, want '01 02'", got)
	}
	if got := unitIDs(Units(mb, "", 2, nil)); got != "01.01 01.02 02.01" {
		t.Errorf("all/combine2 = %q, want '01.01 01.02 02.01'", got)
	}
	us := Units(mb, "1:2", 1, nil)
	if len(us) != 1 || us[0].Disp != "Раздел 2" || !strings.Contains(us[0].Text, "Второй раздел") {
		t.Errorf("1:2 → %+v", us)
	}
}

const notesFB2 = `<?xml version="1.0"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0" xmlns:l="http://www.w3.org/1999/xlink">
<body><section><title><p>Глава</p></title>
<p>Текст со сноской<a l:href="#n1" type="note">1</a> здесь.</p>
</section></body>
<body name="notes"><section id="n1"><p>1 Пояснение к сноске.</p></section></body>
</FictionBook>`

func TestNotesInline(t *testing.T) {
	bodies, err := Parse(strings.NewReader(notesFB2))
	if err != nil {
		t.Fatal(err)
	}
	notes := parseNotes(bodies)
	if notes["n1"] != "Пояснение к сноске." { // leading "1 " stripped
		t.Fatalf("note n1 = %q", notes["n1"])
	}
	us := Units(MainBody(bodies), "", 1, notes)
	if len(us) != 1 {
		t.Fatalf("units = %d", len(us))
	}
	if !strings.Contains(us[0].Text, "Примечание. Пояснение к сноске.") {
		t.Errorf("note not inlined: %q", us[0].Text)
	}
	// The visible ref number "1" must not leak as its own token next to "сноской".
	if strings.Contains(us[0].Text, "сноской1") {
		t.Errorf("ref number leaked: %q", us[0].Text)
	}
	// SEP isolates the footnote into its own chunk(s).
	chunks := Chunk(us[0].Text, 200)
	var hasNoteChunk bool
	for _, c := range chunks {
		if strings.HasPrefix(strings.TrimSpace(c), "Примечание.") {
			hasNoteChunk = true
		}
	}
	if !hasNoteChunk {
		t.Errorf("footnote not isolated into its own chunk: %v", chunks)
	}
}

// The in-body "[N]" + КОММЕНТАРИИ convention (e.g. Russian "11/22/63"): markers are plain text, notes
// live in a bold-headed comments section that must be inlined at the [N] sites and dropped from the tail.
const bracketFB2 = `<?xml version="1.0"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0">
<body><section><title><p>Роман</p></title>
<p>Первый абзац со сноской[1] в тексте.</p>
<p>Второй абзац и ещё сноска[2] тут.</p>
<p><strong>КОММЕНТАРИИ</strong></p>
<empty-line></empty-line>
<p>[1] Пояснение первое.</p>
<p>[2] Пояснение второе.</p>
</section></body>
</FictionBook>`

func TestBracketNotes(t *testing.T) {
	// bracketMode is a package var set by Extract; drive it directly for this Units-level test.
	bracketMode = true
	defer func() { bracketMode = false }()
	bodies, err := Parse(strings.NewReader(bracketFB2))
	if err != nil {
		t.Fatal(err)
	}
	mb := MainBody(bodies)
	notes, skip := parseBracketNotes(mb)
	if notes["1"] != "Пояснение первое." || notes["2"] != "Пояснение второе." {
		t.Fatalf("bracket notes = %+v", notes)
	}
	if len(skip) < 3 { // heading + empty-line + 2 defs
		t.Errorf("skip set too small: %d", len(skip))
	}
	for n := range skip {
		n.skip = true
	}
	us := Units(mb, "", 1, notes)
	if len(us) != 1 {
		t.Fatalf("units = %d", len(us))
	}
	txt := us[0].Text
	if !strings.Contains(txt, "Примечание. Пояснение первое.") || !strings.Contains(txt, "Примечание. Пояснение второе.") {
		t.Errorf("notes not inlined at [N] sites: %q", txt)
	}
	if strings.Contains(txt, "КОММЕНТАРИИ") || strings.Contains(txt, "[1]") || strings.Contains(txt, "[2]") {
		t.Errorf("comment region or raw marker leaked into narration: %q", txt)
	}
	// SEP-isolation → note lands in its own chunk, flagged in the mask.
	chunks, mask := ChunkMask(txt, 200)
	var noteChunks int
	for i, c := range chunks {
		if mask[i] && strings.HasPrefix(strings.TrimSpace(c), "Примечание.") {
			noteChunks++
		}
	}
	if noteChunks != 2 {
		t.Errorf("expected 2 flagged note chunks, got %d (chunks=%v mask=%v)", noteChunks, chunks, mask)
	}
}

func TestChunkPacking(t *testing.T) {
	got := Chunk("Раз. Два. Три четыре пять.", 12)
	// "Раз." (4) then "Два." would be 4+1+4=9≤12 → "Раз. Два."; +"Три четыре пять."(16) exceeds.
	want := []string{"Раз. Два.", "Три четыре пять."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Chunk = %v, want %v", got, want)
	}
}

// Gated on real "Пасынки" — section-based selector + COMBINE.
func TestUnitsPasynki(t *testing.T) {
	f := os.Getenv("HOME") + "/src/f5-spike/runpod-bundle/book.fb2"
	fh, err := os.Open(f)
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	defer fh.Close()
	bodies, _ := Parse(fh)
	mb := MainBody(bodies)

	if got := unitIDs(Units(mb, "1:2 2:1-3 4", 1, nil)); got != "01.02 02.01 02.02 02.03 04" {
		t.Errorf("selector units = %q", got)
	}
	// Part 1 whole vs per-chapter.
	if got := unitIDs(Units(mb, "1", 1, nil)); got != "01" {
		t.Errorf("1/combine1 = %q, want 01", got)
	}
	if got := unitIDs(Units(mb, "1", 2, nil)); got != "01.01 01.02 01.03 01.04" {
		t.Errorf("1/combine2 = %q, want 4 chapters", got)
	}
	// Chapter 2 of book 1 = "2", text mentions Лаура.
	us := Units(mb, "1:2", 1, nil)
	if len(us) != 1 || us[0].Disp != "2" || !strings.Contains(us[0].Text, "Лаура") {
		t.Errorf("1:2 disp=%q textHasЛаура=%v", us[0].Disp, strings.Contains(us[0].Text, "Лаура"))
	}
}

func TestSpokenHeadings(t *testing.T) {
	if got := spokenHeading("КНИГА ПЕРВАЯ", "2", true); got != "КНИГА ПЕРВАЯ. Глава вторая." {
		t.Errorf("first chapter heading = %q", got)
	}
	if got := spokenHeading("КНИГА ПЕРВАЯ", "2", false); got != "Глава вторая." {
		t.Errorf("non-first heading = %q", got)
	}
	if got := ordinalFem(23); got != "двадцать третья" {
		t.Errorf("ordinalFem(23) = %q", got)
	}
	if got := spokenHeading("", "ВЕЛИКИЙ ЗДРАЙЦА", false); got != "ВЕЛИКИЙ ЗДРАЙЦА." {
		t.Errorf("named chapter heading = %q", got)
	}
}
