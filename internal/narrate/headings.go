package narrate

import (
	"regexp"
	"strconv"
)

// Feminine ordinals (глава f.) 1..40; 21..39 compose tens + unit. Ported from fb2_extract.py so a bare
// numeric chapter title ("2") is voiced "Глава вторая", not read as the digit "два".
var ordFem = map[int]string{
	1: "первая", 2: "вторая", 3: "третья", 4: "четвёртая", 5: "пятая", 6: "шестая", 7: "седьмая",
	8: "восьмая", 9: "девятая", 10: "десятая", 11: "одиннадцатая", 12: "двенадцатая", 13: "тринадцатая",
	14: "четырнадцатая", 15: "пятнадцатая", 16: "шестнадцатая", 17: "семнадцатая", 18: "восемнадцатая",
	19: "девятнадцатая", 20: "двадцатая", 30: "тридцатая", 40: "сороковая",
}
var ordTens = map[int]string{20: "двадцать", 30: "тридцать", 40: "сорок"}

func ordinalFem(n int) string {
	if v, ok := ordFem[n]; ok {
		return v
	}
	if n > 20 && n < 40 {
		if u := n % 10; u > 0 {
			return ordTens[(n/10)*10] + " " + ordFem[u]
		}
	}
	return strconv.Itoa(n)
}

var digitsRe = regexp.MustCompile(`^\d+$`)

// spokenHeading is the announcement read before a chapter: the part title once (on its first chapter),
// then the chapter heading — "Глава <ordinal>." for a bare-numeric title, else the title verbatim.
func spokenHeading(partTitle, chapTitle string, first bool) string {
	var chead string
	switch {
	case digitsRe.MatchString(chapTitle):
		chead = "Глава " + ordinalFem(atoi(chapTitle)) + "."
	case chapTitle != "":
		chead = chapTitle + "."
	}
	if first && partTitle != "" {
		return partTitle + ". " + chead
	}
	return chead
}
