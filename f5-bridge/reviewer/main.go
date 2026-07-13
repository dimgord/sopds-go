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

	// list of chapters + the ambiguous-homograph vocabulary (client computes per-word flags).
	http.HandleFunc("/api/list", func(w http.ResponseWriter, r *http.Request) {
		files, _ := titles()
		homs := lines(filepath.Join(reviewDir, "_homographs.txt"))
		writeJSON(w, map[string]any{"files": files, "homographs": homs})
	})

	// one chapter's aligned raw + stressed lines.
	http.HandleFunc("/api/file", func(w http.ResponseWriter, r *http.Request) {
		_, by := titles()
		name := r.URL.Query().Get("name")
		if _, ok := by[name]; !ok {
			http.Error(w, "unknown file", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"raw":      lines(filepath.Join(reviewDir, name+".raw.txt")),
			"stressed": lines(filepath.Join(reviewDir, name+".txt")),
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
