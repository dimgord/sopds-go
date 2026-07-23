package narrate

import (
	"os"
	"strings"
	"testing"
)

// Synthetic flat book: one section, parts/chapters as bold heading paragraphs.
const flatFB2 = `<?xml version="1.0" encoding="UTF-8"?>
<FictionBook xmlns="http://www.gribuser.ru/xml/fictionbook/2.0"><body><section>
<p><strong>Часть 1</strong></p>
<p><strong>Раздел 1</strong></p>
<p>Текст первого раздела.</p>
<p>Ещё текст.</p>
<p><strong>Раздел 2</strong></p>
<p>Второй раздел тут.</p>
<p><strong>Часть 2</strong></p>
<p><strong>Раздел 3</strong></p>
<p>Третий раздел.</p>
</section></body></FictionBook>`

func TestParseFlatBoldHeadings(t *testing.T) {
	bodies, err := Parse(strings.NewReader(flatFB2))
	if err != nil {
		t.Fatal(err)
	}
	mb := MainBody(bodies)
	if mb == nil {
		t.Fatal("no main body")
	}
	secs := mb.sections()
	if len(secs) != 1 {
		t.Fatalf("flat book: want 1 section, got %d", len(secs))
	}
	// Count bold heading paragraphs + their texts.
	var bolds []string
	for _, c := range secs[0].children {
		if c.kind == kParagraph && c.bold {
			bolds = append(bolds, c.text)
		}
	}
	want := []string{"Часть 1", "Раздел 1", "Раздел 2", "Часть 2", "Раздел 3"}
	if strings.Join(bolds, "|") != strings.Join(want, "|") {
		t.Errorf("bold headings = %v, want %v", bolds, want)
	}
	// Non-heading paragraphs must NOT be flagged bold.
	for _, c := range secs[0].children {
		if c.kind == kParagraph && !c.bold && c.text == "" {
			t.Errorf("empty non-bold paragraph leaked")
		}
	}
}

// Gated on the real "Пасынки восьмой заповеди" FB2 — verifies section-based structure detection.
func TestParsePasynkiStructure(t *testing.T) {
	f := os.Getenv("HOME") + "/src/f5-spike/runpod-bundle/book.fb2"
	fh, err := os.Open(f)
	if err != nil {
		t.Skipf("skip: %v", err)
	}
	defer fh.Close()
	bodies, err := Parse(fh)
	if err != nil {
		t.Fatal(err)
	}
	mb := MainBody(bodies)
	tops := mb.sections()
	if len(tops) != 4 {
		t.Fatalf("Пасынки: want 4 top sections, got %d", len(tops))
	}
	// [1] КНИГА ПЕРВАЯ → 4 nested; [4] ЭПИЛОГ → 0 nested.
	if got := len(tops[0].sections()); got != 4 {
		t.Errorf("book 1 nested = %d, want 4", got)
	}
	if got := len(tops[3].sections()); got != 0 {
		t.Errorf("эпилог nested = %d, want 0", got)
	}
	if !strings.Contains(tops[0].titleText(), "КНИГА ПЕРВАЯ") {
		t.Errorf("book 1 title = %q", tops[0].titleText())
	}
	if tops[3].titleText() != "ЭПИЛОГ" {
		t.Errorf("part 4 title = %q, want ЭПИЛОГ", tops[3].titleText())
	}
}
