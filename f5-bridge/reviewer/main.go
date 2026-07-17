// f5-stress-reviewer — a tiny local web tool to proofread RUAccent stress before F5 synthesis.
//
// The review dir holds 1:1 line-aligned pairs per chapter: NN_<safe>.raw.txt (reference, as
// extracted) and NN_<safe>.txt (RUAccent-stressed, editable). This serves a two-pane UI that shows
// them side by side, flags ambiguous ё-homographs (from _homographs.txt), and saves edits back to
// the .txt (keeping a one-time .orig backup). No build step, stdlib only.
//
//	go run . -dir ~/f5-review [-port 8765]
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed index.html
var indexHTML []byte

var reviewDir string

type fileMeta struct {
	NN    string `json:"nn"`
	Safe  string `json:"safe"`
	Title string `json:"title"`
}

// titles reads _titles.tsv → ordered [NN, safe, title]; also the set of valid "NN_safe" base names.
func titles() ([]fileMeta, map[string]fileMeta) {
	out := []fileMeta{}
	by := map[string]fileMeta{}
	data, err := os.ReadFile(filepath.Join(reviewDir, "_titles.tsv"))
	if err != nil {
		return out, by
	}
	for _, ln := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		f := strings.SplitN(ln, "\t", 3)
		if len(f) < 3 {
			continue
		}
		m := fileMeta{NN: f[0], Safe: f[1], Title: f[2]}
		out = append(out, m)
		by[f[0]+"_"+f[1]] = m
	}
	return out, by
}

func lines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{} // absent file → empty (never JSON null, so the client can .map it)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// ---- pristine-baseline alignment: recover each CURRENT row's original (pre-edit) stressed text ----
// The .orig backup is chunked differently (rechunk shifts line indices), so we align by WORD, not line:
// a difflib-style longest-matching-block pass over the folded word streams maps every current word to
// its original counterpart; we then reassemble the original text per current row. Chunk-agnostic, so it
// survives any rechunk. Classification stays client-side (no duplicated dicts) — we only return text.

var vowelSet = map[rune]bool{}

func init() {
	for _, r := range "аеёиоуыэюяАЕЁИОУЫЭЮЯ" {
		vowelSet[r] = true
	}
}

func toPlusGo(s string) string { // combining acute (U+0301) after a vowel → "+vowel"; stray acute dropped
	rs := []rune(s)
	out := make([]rune, 0, len(rs))
	for i := 0; i < len(rs); i++ {
		if rs[i] == '́' {
			continue
		}
		if i+1 < len(rs) && rs[i+1] == '́' && vowelSet[rs[i]] {
			out = append(out, '+', rs[i])
			continue
		}
		out = append(out, rs[i])
	}
	return string(out)
}

func foldWord(w string) string { // lower, drop '+', ё→е, trim edge punctuation — the alignment key
	var b strings.Builder
	for _, r := range strings.ToLower(w) {
		if r == '+' {
			continue
		}
		if r == 'ё' {
			r = 'е'
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "«»\"'([{.,!?;:—-–…)]}")
}

func flatWords(lines []string) (flat []string, lineOf []int) {
	for i, l := range lines {
		for _, w := range strings.Fields(toPlusGo(l)) {
			flat = append(flat, w)
			lineOf = append(lineOf, i)
		}
	}
	return
}

// longestMatch / matchingBlocks: a compact port of difflib.SequenceMatcher (no junk heuristics).
func longestMatch(a, b []string, b2j map[string][]int, alo, ahi, blo, bhi int) (int, int, int) {
	besti, bestj, bestsize := alo, blo, 0
	j2len := map[int]int{}
	for i := alo; i < ahi; i++ {
		newj2len := map[int]int{}
		for _, j := range b2j[a[i]] {
			if j < blo {
				continue
			}
			if j >= bhi {
				break
			}
			k := j2len[j-1] + 1
			newj2len[j] = k
			if k > bestsize {
				besti, bestj, bestsize = i-k+1, j-k+1, k
			}
		}
		j2len = newj2len
	}
	return besti, bestj, bestsize
}

func matchingBlocks(a, b []string) [][3]int {
	b2j := map[string][]int{}
	for j, w := range b {
		b2j[w] = append(b2j[w], j)
	}
	type span struct{ alo, ahi, blo, bhi int }
	stack := []span{{0, len(a), 0, len(b)}}
	var res [][3]int
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		i, j, k := longestMatch(a, b, b2j, s.alo, s.ahi, s.blo, s.bhi)
		if k > 0 {
			res = append(res, [3]int{i, j, k})
			if s.alo < i && s.blo < j {
				stack = append(stack, span{s.alo, i, s.blo, j})
			}
			if i+k < s.ahi && j+k < s.bhi {
				stack = append(stack, span{i + k, s.ahi, j + k, s.bhi})
			}
		}
	}
	return res
}

// alignedOrig: for each CURRENT line, its original (pre-edit) stressed text, pulled word-by-word from .orig.
func alignedOrig(origLines, curLines []string) []string {
	ow, _ := flatWords(origLines)
	cw, clineOf := flatWords(curLines)
	of := make([]string, len(ow))
	cf := make([]string, len(cw))
	for i, w := range ow {
		of[i] = foldWord(w)
	}
	for i, w := range cw {
		cf[i] = foldWord(w)
	}
	origWord := make([]string, len(cw))
	copy(origWord, cw) // default: unmatched current word maps to itself
	for _, bl := range matchingBlocks(of, cf) {
		ai, bj, k := bl[0], bl[1], bl[2]
		for t := 0; t < k; t++ {
			origWord[bj+t] = ow[ai+t]
		}
	}
	out := make([]string, len(curLines))
	for wi, li := range clineOf {
		if out[li] != "" {
			out[li] += " "
		}
		out[li] += origWord[wi]
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	home, _ := os.UserHomeDir()
	flag.StringVar(&reviewDir, "dir", filepath.Join(home, "f5-review"), "review directory (NN_*.raw.txt + NN_*.txt)")
	port := flag.Int("port", 8765, "listen port")
	flag.Parse()
	if _, err := os.Stat(filepath.Join(reviewDir, "_titles.tsv")); err != nil {
		log.Fatalf("no _titles.tsv in %s (run MODE=stress first)", reviewDir)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})

	// chapters + the flag vocabularies (client computes per-word flags): _homographs.txt = RUAccent's
	// ё/stress homograph set (ё flags); _omographs.txt / _unknown.txt = dict_flags.py sidecars
	// (stress-homograph glance + genuine unstressed skips).
	http.HandleFunc("/api/list", func(w http.ResponseWriter, r *http.Request) {
		files, _ := titles()
		writeJSON(w, map[string]any{
			"files":      files,
			"homographs": lines(filepath.Join(reviewDir, "_homographs.txt")),
			"omographs":  lines(filepath.Join(reviewDir, "_omographs.txt")),
			"unknown":    lines(filepath.Join(reviewDir, "_unknown.txt")),
		})
	})

	// one chapter's aligned raw + stressed lines.
	http.HandleFunc("/api/file", func(w http.ResponseWriter, r *http.Request) {
		_, by := titles()
		name := r.URL.Query().Get("name")
		if _, ok := by[name]; !ok {
			http.Error(w, "unknown file", http.StatusBadRequest)
			return
		}
		stressed := lines(filepath.Join(reviewDir, name+".txt"))
		orig := lines(filepath.Join(reviewDir, name+".txt.orig"))
		aligned := stressed // no .orig backup yet → nothing edited, original == current
		if len(orig) > 0 {
			aligned = alignedOrig(orig, stressed)
		}
		writeJSON(w, map[string]any{
			"raw":      lines(filepath.Join(reviewDir, name+".raw.txt")),
			"stressed": stressed,
			"orig":     aligned, // pre-edit stressed, aligned 1:1 to current rows → client derives original type
		})
	})

	// save edited stressed lines back to NN_safe.txt (one-time .orig backup first).
	http.HandleFunc("/api/save", func(w http.ResponseWriter, r *http.Request) {
		_, by := titles()
		name := r.URL.Query().Get("name")
		if _, ok := by[name]; !ok || r.Method != http.MethodPost {
			http.Error(w, "bad save", http.StatusBadRequest)
			return
		}
		var body struct {
			Stressed []string `json:"stressed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		txt := filepath.Join(reviewDir, name+".txt")
		orig := txt + ".orig"
		if _, err := os.Stat(orig); os.IsNotExist(err) {
			if cur, err := os.ReadFile(txt); err == nil {
				_ = os.WriteFile(orig, cur, 0o644)
			}
		}
		if err := os.WriteFile(txt, []byte(strings.Join(body.Stressed, "\n")+"\n"), 0o644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "lines": len(body.Stressed)})
	})

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("stress reviewer → http://%s  (dir: %s)", addr, reviewDir)
	log.Fatal(http.ListenAndServe(addr, nil))
}
