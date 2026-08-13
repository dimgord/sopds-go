// Command audiorepack finds solid .7z audiobooks and repacks them non-solid.
//
// A solid archive puts every track in one compression block, so serving a single
// track has to decompress every track ahead of it — seconds of delay per click.
// See PROGRESS.md Rev 132. Reports only unless -repack is given.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

func main() {
	var (
		repack  = flag.Bool("repack", false, "repack the solid archives in place (default: report only)")
		workers = flag.Int("j", min(4, runtime.NumCPU()), "archives examined in parallel")
		quiet   = flag.Bool("q", false, "list only the solid archives, no per-archive OK lines")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: audiorepack [-repack] [-j N] [-q] <dir>...\n\n")
		fmt.Fprintf(os.Stderr, "Walks each dir for .7z archives and reports the solid ones.\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if _, err := exec.LookPath("7zz"); err != nil {
		fmt.Fprintln(os.Stderr, "audiorepack: 7zz not found in PATH")
		os.Exit(1)
	}

	archives, err := findArchives(flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "audiorepack: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%d archive(s) to examine\n\n", len(archives))

	var (
		mu     sync.Mutex
		solid  []string
		failed int
	)
	sem := make(chan struct{}, max(1, *workers))
	var wg sync.WaitGroup
	for _, a := range archives {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			entries, err := listEntries(path)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fmt.Printf("ERR   %s: %v\n", path, err)
				failed++
				return
			}
			blocks, files := countBlocks(entries)
			if files > 1 && blocks < files {
				fmt.Printf("SOLID %s (%d files in %d block(s))\n", path, files, blocks)
				solid = append(solid, path)
			} else if !*quiet {
				fmt.Printf("ok    %s (%d files, %d block(s))\n", path, files, blocks)
			}
		}(a)
	}
	wg.Wait()

	sort.Strings(solid)
	fmt.Printf("\n%d solid, %d fine, %d unreadable\n", len(solid), len(archives)-len(solid)-failed, failed)
	if len(solid) == 0 || !*repack {
		if len(solid) > 0 {
			fmt.Println("\nre-run with -repack to fix them in place")
		}
		return
	}

	// Repacking is serial on purpose: it extracts a whole audiobook to disk, and
	// several at once would thrash both the disk and the free space it needs.
	fmt.Println()
	for _, path := range solid {
		if err := repackArchive(path); err != nil {
			fmt.Printf("FAIL  %s: %v\n", path, err)
			failed++
			continue
		}
		fmt.Printf("done  %s\n", path)
	}
	if failed > 0 {
		os.Exit(1)
	}
}

func findArchives(dirs []string) ([]string, error) {
	var out []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".7z") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

// entry is one record of `7zz l -slt` output. Block is -1 when the record
// carries no Block line (directories, and stored entries in some archives).
type entry struct {
	path  string
	dir   bool
	block int
}

func listEntries(archive string) ([]entry, error) {
	out, err := exec.Command("7zz", "l", "-slt", archive).Output()
	if err != nil {
		return nil, fmt.Errorf("7zz l: %w", err)
	}
	return parseSLT(string(out)), nil
}

// parseSLT reads `7zz l -slt` output. Records are separated by blank lines and
// only start once the "----------" header line has gone by.
func parseSLT(out string) []entry {
	var (
		entries []entry
		cur     entry
		started bool
		inRec   bool
	)
	cur.block = -1
	flush := func() {
		if inRec && cur.path != "" {
			entries = append(entries, cur)
		}
		cur = entry{block: -1}
		inRec = false
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if !started {
			if strings.HasPrefix(line, "----------") {
				started = true
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		key, val, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		switch key {
		case "Path":
			flush()
			inRec = true
			cur.path = val
		case "Attributes":
			cur.dir = strings.HasPrefix(val, "D")
		case "Folder":
			// 7-Zip marks directory records with Folder = +
			if val == "+" {
				cur.dir = true
			}
		case "Block":
			if n, err := strconv.Atoi(val); err == nil {
				cur.block = n
			}
		}
	}
	flush()
	return entries
}

// countBlocks returns how many distinct compression blocks the files occupy and
// how many file (non-directory) entries there are. blocks < files means solid.
func countBlocks(entries []entry) (blocks, files int) {
	seen := map[int]bool{}
	for _, e := range entries {
		if e.dir {
			continue
		}
		files++
		if e.block >= 0 {
			seen[e.block] = true
		} else {
			// No Block line: the entry is stored on its own, which is what
			// non-solid looks like. Count it as its own block.
			blocks++
		}
	}
	return blocks + len(seen), files
}

// repackArchive rebuilds one archive store+non-solid, next to the original, and
// swaps it in only after the new one lists the same files. The original is left
// untouched on any error.
func repackArchive(archive string) error {
	dir := filepath.Dir(archive)
	stage, err := os.MkdirTemp(dir, ".audiorepack-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	if out, err := exec.Command("7zz", "x", "-y", "-o"+stage, archive).CombinedOutput(); err != nil {
		return fmt.Errorf("extract: %v: %s", err, lastLine(out))
	}
	// The archive holds a single top-level folder; pack that, not the temp dir,
	// so the internal paths come out identical to the original.
	top, err := singleChild(stage)
	if err != nil {
		return err
	}

	newArc := filepath.Join(dir, "."+filepath.Base(archive)+".new")
	_ = os.Remove(newArc)
	defer os.Remove(newArc)
	// -mx=0 -ms=off: audio is already compressed, so store it, one block per file.
	if out, err := exec.Command("7zz", "a", "-mx=0", "-ms=off", newArc, top).CombinedOutput(); err != nil {
		return fmt.Errorf("pack: %v: %s", err, lastLine(out))
	}

	before, err := listEntries(archive)
	if err != nil {
		return err
	}
	after, err := listEntries(newArc)
	if err != nil {
		return err
	}
	if err := sameFiles(before, after); err != nil {
		return err
	}
	if blocks, files := countBlocks(after); files > 1 && blocks < files {
		return fmt.Errorf("repacked archive is still solid (%d files, %d blocks)", files, blocks)
	}
	return os.Rename(newArc, archive)
}

func singleChild(dir string) (string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(ents) != 1 {
		return "", fmt.Errorf("expected one top-level folder in the archive, found %d", len(ents))
	}
	return filepath.Join(dir, ents[0].Name()), nil
}

func sameFiles(before, after []entry) error {
	names := func(es []entry) []string {
		var n []string
		for _, e := range es {
			if !e.dir {
				n = append(n, e.path)
			}
		}
		sort.Strings(n)
		return n
	}
	a, b := names(before), names(after)
	if len(a) != len(b) {
		return fmt.Errorf("file count changed: %d → %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			return fmt.Errorf("file list changed: %q → %q", a[i], b[i])
		}
	}
	return nil
}

func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
