package scanner

import (
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

func TestDecodeArchiveName(t *testing.T) {
	// A lib.rus.ec-style Cyrillic filename, encoded to CP866 as the downloaded
	// archives store it (no UTF-8 flag → archive/zip returns these raw bytes).
	want := "Бауэр Сьюзен Уайс - История уязвимости.fb2"
	cp866, err := charmap.CodePage866.NewEncoder().String(want)
	if err != nil {
		t.Fatalf("encode setup: %v", err)
	}
	if utf8.ValidString(cp866) {
		t.Fatal("setup string unexpectedly valid UTF-8; decode path not exercised")
	}

	if got := decodeArchiveName(cp866); got != want {
		t.Errorf("CP866 not decoded:\n got=%q\nwant=%q", got, want)
	} else if !utf8.ValidString(got) {
		t.Errorf("decoded result is not valid UTF-8: %q", got)
	}

	// Already-valid UTF-8 passes through untouched (e.g. Info-ZIP-created archives).
	utf := "Sense Митя, Red Полина.fb2"
	if got := decodeArchiveName(utf); got != utf {
		t.Errorf("valid UTF-8 altered: got=%q want=%q", got, utf)
	}

	// Pure ASCII (numeric ids) untouched.
	if got := decodeArchiveName("345779.fb2"); got != "345779.fb2" {
		t.Errorf("ascii altered: %q", got)
	}

	// Undecodable garbage must still come out valid UTF-8 — never hand raw
	// invalid bytes to a UTF-8 Postgres column.
	if got := decodeArchiveName("\xff\xfe\xfd\xfc"); !utf8.ValidString(got) {
		t.Errorf("garbage not sanitized to valid UTF-8: %q", got)
	}
}

func TestClampRunes(t *testing.T) {
	// Cyrillic: 5 runes / 10 bytes — must clamp by runes, matching varchar(N).
	if got := clampRunes("абвгд", 3); got != "абв" {
		t.Errorf("clampRunes cyrillic: got=%q want=%q", got, "абв")
	}
	if n := utf8.RuneCountInString(clampRunes(strings.Repeat("я", 300), 256)); n != 256 {
		t.Errorf("clampRunes limited to %d runes, want 256", n)
	}
	if got := clampRunes("short", 256); got != "short" {
		t.Errorf("short string altered: %q", got)
	}
	if got := clampRunes("x", 0); got != "x" {
		t.Errorf("n<=0 should be a no-op: %q", got)
	}
}
