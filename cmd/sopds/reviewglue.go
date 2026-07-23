package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// runNDJSONReqs builds the F5 daemon's request stream (native replacement for the shell's
// `python3 -c json.dumps` loop): for every stressed chunk in review/<id>_<safe>.txt it emits one
// NDJSON line {"text":<chunk>,"output":<work>/p<id>_c<NNNNN>.wav} to stdout. Args: <review> <work>.
func runNDJSONReqs(cmd *cobra.Command, args []string) error {
	reviewDir, workDir := args[0], args[1]
	units, err := readTitles(reviewDir)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false) // keep UTF-8 as-is (like python ensure_ascii=False)
	gidx := 0
	for _, u := range units {
		f, err := os.Open(filepath.Join(reviewDir, u.id+"_"+u.safe+".txt"))
		if err != nil {
			continue // a unit with no stressed text is skipped, as in the shell
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			gidx++
			out := fmt.Sprintf("%s/p%s_c%05d.wav", workDir, u.id, gidx)
			if err := enc.Encode(map[string]string{"text": line, "output": out}); err != nil {
				f.Close()
				return err
			}
		}
		f.Close()
	}
	return nil
}

// runCheckYo writes the reviewer's ё-flag report (native replacement for the shell's python heredoc):
// for each stressed chunk, flag any word where RUAccent ADDED a ё and the (de-stressed, lowercased)
// base word is a known ambiguous homograph — the ones worth eyeballing. Args: <review> <homographs>.
func runCheckYo(cmd *cobra.Command, args []string) error {
	reviewDir, homFile := args[0], args[1]
	homs := map[string]bool{}
	if data, err := os.ReadFile(homFile); err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				homs[l] = true
			}
		}
	}
	strip := func(w string) string { return strings.ReplaceAll(w, "+", "") }
	base := func(w string) string { return strings.Trim(strings.ToLower(strip(w)), `.,!?;:»«"()—-`) }

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	fmt.Fprintln(w, "part\tchunk\tword\t(ambiguous ё-homograph — verify noun/verb/case in the .txt)")

	txts, _ := filepath.Glob(filepath.Join(reviewDir, "*_*.txt"))
	sort.Strings(txts)
	for _, txt := range txts {
		if strings.HasSuffix(txt, ".raw.txt") {
			continue
		}
		raw := strings.TrimSuffix(txt, ".txt") + ".raw.txt"
		rawLines, err1 := readLines(raw)
		txtLines, err2 := readLines(txt)
		if err1 != nil || err2 != nil {
			continue
		}
		part := strings.SplitN(filepath.Base(txt), "_", 2)[0]
		n := len(rawLines)
		if len(txtLines) < n {
			n = len(txtLines)
		}
		for i := 0; i < n; i++ {
			aw, bw := strings.Fields(rawLines[i]), strings.Fields(txtLines[i])
			if len(aw) != len(bw) {
				continue
			}
			for j := range aw {
				x, y := aw[j], bw[j]
				if strings.Contains(strings.ToLower(strip(y)), "ё") &&
					!strings.Contains(strings.ToLower(x), "ё") && homs[base(x)] {
					fmt.Fprintf(w, "%s\t%d\t%s→%s\n", part, i+1, x, strip(y))
				}
			}
		}
	}
	return nil
}

type titleUnit struct{ id, safe, title string }

func readTitles(reviewDir string) ([]titleUnit, error) {
	f, err := os.Open(filepath.Join(reviewDir, "_titles.tsv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []titleUnit
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.SplitN(sc.Text(), "\t", 3)
		if len(p) < 2 {
			continue
		}
		u := titleUnit{id: p[0], safe: p[1]}
		if len(p) == 3 {
			u.title = p[2]
		}
		out = append(out, u)
	}
	return out, sc.Err()
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}
