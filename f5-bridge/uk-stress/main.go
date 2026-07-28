// uk-stress — native Ukrainian word-stress annotator for the F5 audiobook pipeline.
//
// Reads plain Ukrainian text on stdin, writes "+"-stressed text on stdout (one line
// per input line) — a drop-in for the RUAccent `sopds-tts-rs stress` step in
// fb2-to-f5.sh's `uk-stress` mode. The "+" goes before the stressed vowel
// (Дніпр+о), which is exactly what our uk_q6 "+"-model reads.
//
// Dictionary: the VESUM-based lang-uk/ukrainian-word-stress-dictionary (~2.8M forms),
// preprocessed to `base<TAB>+form` (uk_stress.tsv) + an ambiguous-stress list
// (uk_stress_homographs.txt). Unknown words AND homographs are left UNSTRESSED — the
// human fixes those in the reviewer (homographs need sentence context we don't have
// here). NO Python: the dict is plain data, this is one static Go binary.
//
//	uk-stress -dict uk_stress.tsv [-homographs uk_stress_homographs.txt] < raw.txt > out.txt
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"unicode"
)

func loadDict(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "uk-stress: cannot open dict %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()
	m := make(map[string]string, 3_000_000)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		if i := strings.IndexByte(sc.Text(), '\t'); i > 0 {
			m[sc.Text()[:i]] = sc.Text()[i+1:]
		}
	}
	return m
}

func loadHomo(path string) map[string]bool {
	m := map[string]bool{}
	if path == "" {
		return m
	}
	f, err := os.Open(path)
	if err != nil {
		return m // optional
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		if i := strings.IndexByte(sc.Text(), '\t'); i > 0 {
			m[sc.Text()[:i]] = true
		}
	}
	return m
}

// a "word" is a maximal run of letters plus the apostrophe (сім'я) — hyphens split
// compounds so each part is stressed on its own (синьо-жо́втий).
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || r == '\'' || r == '’' || r == 'ʼ'
}

// applyCase re-inserts the "+" from the (lowercase) dict form into the original word,
// preserving the word's own casing. form == base with "+" inserted, base == lower(w).
func applyCase(w, form string) string {
	wr := []rune(w)
	var b strings.Builder
	k := 0
	for _, r := range form {
		if r == '+' {
			b.WriteByte('+')
			continue
		}
		if k < len(wr) {
			b.WriteRune(wr[k])
			k++
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stressWord(w string, dict, defaults map[string]string, homo map[string]bool) string {
	base := strings.ToLower(w)
	if homo[base] {
		// ambiguous stress: apply the real-usage default if we have one (the reviewer
		// colours these for you to verify), else leave unstressed for manual marking.
		if form, ok := defaults[base]; ok {
			return applyCase(w, form)
		}
		return w
	}
	if form, ok := dict[base]; ok {
		return applyCase(w, form)
	}
	return w // unknown → unstressed
}

func stressLine(line string, dict, defaults map[string]string, homo map[string]bool) string {
	rs := []rune(line)
	var b strings.Builder
	for i := 0; i < len(rs); {
		if isWordRune(rs[i]) {
			j := i
			for j < len(rs) && isWordRune(rs[j]) {
				j++
			}
			b.WriteString(stressWord(string(rs[i:j]), dict, defaults, homo))
			i = j
		} else {
			b.WriteRune(rs[i])
			i++
		}
	}
	return b.String()
}

func main() {
	dictPath := flag.String("dict", "", "path to uk_stress.tsv (base<TAB>+form)")
	homoPath := flag.String("homographs", "", "path to uk_stress_homographs.txt (ambiguous words)")
	defPath := flag.String("defaults", "", "path to uk_stress_defaults.tsv (real-usage default for homographs; the reviewer flags these for verification)")
	flag.Parse()
	if *dictPath == "" {
		fmt.Fprintln(os.Stderr, "uk-stress: -dict is required")
		os.Exit(1)
	}
	dict := loadDict(*dictPath)
	homo := loadHomo(*homoPath)
	defaults := map[string]string{}
	if *defPath != "" {
		defaults = loadDict(*defPath)
	}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for in.Scan() {
		out.WriteString(stressLine(in.Text(), dict, defaults, homo))
		out.WriteByte('\n')
	}
}
