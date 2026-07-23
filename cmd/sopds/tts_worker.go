package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/dimgord/sopds-go/internal/config"
	"github.com/dimgord/sopds-go/internal/database"
	"github.com/dimgord/sopds-go/internal/infrastructure/persistence"
	"github.com/dimgord/sopds-go/internal/scanner"
	"github.com/spf13/cobra"
)

// runTTSWorker auto-fulfills the most-requested pending audio requests with F5-TTS. It's a SEPARATE
// process (run as the F5-env user on the GPU box, e.g. from a systemd timer) — the web server stays in
// request mode. One book per pass by default (the GPU is serial); a systemd oneshot won't overlap itself.
//
// Per book: pick the FB2 → f5-bridge/fb2-to-f5.sh (stress + native F5 synth, per-language voice) →
// 7z the chapter MP3s into library.root/<output_subdir> → scan → link the text book to the new
// audiobook (SetTTSAudioID). Only the "auto" review mode synthesizes here; "gate" stops after stress,
// staging the review text under library.root/.tts-review/<book_id>/ — the operator proofreads it
// (f5-bridge/reviewer) and finishes it with `sopds tts-resume <book_id>`.
func runTTSWorker(cmd *cobra.Command, args []string) error {
	// The app redirects the default logger to a file (setupLogging); for an interactive/timer CLI
	// the operator wants to SEE the worker's progress, so send it to stderr.
	log.SetOutput(os.Stderr)

	maxBooks, _ := cmd.Flags().GetInt("max")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	wc := cfg.TTS.Worker

	if len(wc.Languages) == 0 {
		return fmt.Errorf("tts.worker.languages is empty — configure at least one language's f5_model")
	}
	script := wc.Script
	if script == "" {
		script = filepath.Join(expandHome(wc.F5Home), "fb2-to-f5.sh")
	}
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("fb2-to-f5.sh not found at %s (set tts.worker.script or .f5_home): %w", script, err)
	}

	svc, err := ttsService()
	if err != nil {
		return err
	}
	defer svc.Close()
	ctx := context.Background()

	pending, err := svc.PendingTTSRequests(ctx) // most-requested first
	if err != nil {
		return err
	}

	done := 0
	for _, p := range pending {
		if done >= maxBooks {
			break
		}
		if p.Requests < wc.Threshold {
			break // sorted desc — nothing below here meets the threshold either
		}
		book, err := svc.GetBook(ctx, p.BookID)
		if err != nil {
			log.Printf("tts-worker: skip book %d: %v", p.BookID, err)
			continue
		}
		lang := normLang(book.Lang)
		lc, ok := wc.Languages[lang]
		if !ok || lc.F5Model == "" {
			log.Printf("tts-worker: skip %q (book %d): no f5_model for lang %q", book.Title, book.ID, lang)
			continue
		}

		log.Printf("tts-worker: fulfilling %q (book %d, lang %s, %d requests)", book.Title, book.ID, lang, p.Requests)
		if err := fulfill(ctx, svc, wc, lc, script, book, dryRun); err != nil {
			log.Printf("tts-worker: FAILED %q (book %d): %v", book.Title, book.ID, err)
			continue
		}
		done++
	}
	if done == 0 {
		log.Printf("tts-worker: nothing to do (no pending book ≥ threshold %d with a configured language)", wc.Threshold)
	}
	return nil
}

// fulfill runs one book through the pipeline. On success the text book points at the new audiobook.
func fulfill(ctx context.Context, svc *persistence.Service, wc config.WorkerConfig, lc config.WorkerLangConfig, script string, book *database.Book, dryRun bool) (err error) {
	// 1. FB2 → temp file for fb2-to-f5.sh.
	fb2, cleanup, err := extractFB2(book)
	if err != nil {
		return fmt.Errorf("extract fb2: %w", err)
	}
	defer cleanup()

	// 2. Where the chapter MP3s go. Auto mode: a scratch temp, removed on success (kept on failure so
	// review/_ruaccent.log survives). Gate mode: a STABLE hidden staging dir keyed by book id, kept
	// for proofreading + `tts-resume`.
	gate := wc.ReviewGate()
	var outDir string
	if gate {
		outDir = reviewDirFor(book.ID)
		if err = os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
	} else {
		outDir, err = os.MkdirTemp("", "ttsgen-")
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				log.Printf("  (kept %s for debugging — check review/_ruaccent.log)", outDir)
			} else {
				os.RemoveAll(outDir)
			}
		}()
	}

	mode := "all"
	if gate {
		mode = "stress" // stop after stress; the operator proofreads, then `sopds tts-resume <id>`
	}
	env := f5Env(wc, lc, mode)
	shArgs := []string{script, fb2, outDir}

	if dryRun {
		log.Printf("  [dry-run] bash %s\n    env: %s", strings.Join(shArgs, " "), strings.Join(env, " "))
		return nil
	}

	// 3. Generate (long — hours on a GTX 1070).
	c := exec.CommandContext(ctx, "bash", shArgs...)
	c.Env = append(os.Environ(), env...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err = c.Run(); err != nil {
		return fmt.Errorf("fb2-to-f5.sh: %w", err)
	}

	if gate {
		log.Printf("  review gate: proofread %s/review", outDir)
		log.Printf("               (cd f5-bridge/reviewer && go run . -dir %s/review), then: sopds tts-resume %d", outDir, book.ID)
		return nil // outDir persists (not removed) for the resume
	}

	// 4-6. Package → scan → link.
	return finishAudiobook(ctx, svc, wc, book, outDir)
}

// reviewDirFor is the hidden, per-book staging dir where gate-mode stress output waits for
// proofreading (the scanner skips dot-dirs). Co-located with the library so mp3→.7z stays on one FS.
func reviewDirFor(bookID int64) string {
	return filepath.Join(cfg.Library.Root, ".tts-review", strconv.FormatInt(bookID, 10))
}

// finishAudiobook packages outDir's chapter MP3s into a .7z, rescans so it becomes an audiobook, and
// links the text book to it. Shared by the auto path and `tts-resume`.
func finishAudiobook(ctx context.Context, svc *persistence.Service, wc config.WorkerConfig, book *database.Book, outDir string) error {
	archive, err := packageAudiobook(outDir, book.Title, filepath.Join(cfg.Library.Root, wc.OutputSubdir))
	if err != nil {
		return fmt.Errorf("package: %w", err)
	}
	log.Printf("  packaged → %s", archive)

	if err := scanner.New(cfg, svc).ScanAll(ctx); err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	audios, err := svc.ListAudiobooks(ctx, 1) // newest first = the one we just added
	if err != nil || len(audios) == 0 {
		return fmt.Errorf("locate new audiobook after scan: %v", err)
	}
	audioID := audios[0].BookID
	if err := svc.SetTTSAudioID(ctx, book.ID, &audioID); err != nil {
		return fmt.Errorf("link: %w", err)
	}
	log.Printf("  ✓ linked text book %d → audiobook %d (%s)", book.ID, audioID, audios[0].Title)
	return nil
}

// runTTSResume finishes a review-gated book: after the operator proofreads the staged stressed text,
// synthesize it → package → scan → link, then clear the staging dir. `sopds tts-resume <book_id>`.
func runTTSResume(cmd *cobra.Command, args []string) error {
	log.SetOutput(os.Stderr)
	bookID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("book id must be an integer: %w", err)
	}
	wc := cfg.TTS.Worker
	script := wc.Script
	if script == "" {
		script = filepath.Join(expandHome(wc.F5Home), "fb2-to-f5.sh")
	}
	outDir := reviewDirFor(bookID)
	if _, err := os.Stat(filepath.Join(outDir, "review", "_titles.tsv")); err != nil {
		return fmt.Errorf("no staged review for book %d at %s — run the worker in gate mode first", bookID, outDir)
	}

	svc, err := ttsService()
	if err != nil {
		return err
	}
	defer svc.Close()
	ctx := context.Background()
	book, err := svc.GetBook(ctx, bookID)
	if err != nil {
		return fmt.Errorf("get book %d: %w", bookID, err)
	}
	lc := wc.Languages[normLang(book.Lang)]

	log.Printf("tts-resume: synthesizing proofread %q (book %d)", book.Title, bookID)
	// MODE=synth reads outDir/review; the FB2 arg is unused in synth mode.
	c := exec.CommandContext(ctx, "bash", script, "resume", outDir)
	c.Env = append(os.Environ(), f5Env(wc, lc, "synth")...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("fb2-to-f5.sh synth: %w", err)
	}
	if err := finishAudiobook(ctx, svc, wc, book, outDir); err != nil {
		return err
	}
	_ = os.RemoveAll(outDir) // clear the staging on success
	return nil
}

// extractFB2 writes the book's FB2 to a temp file (extracting from a .zip/.7z archive if needed) and
// returns its path plus a cleanup func. NOTE: the on-disk layout (Path=archive/dir, Filename=entry) is
// assumed from the scanner's model — verify against the real library layout on Fedya.
func extractFB2(book *database.Book) (string, func(), error) {
	data, err := readBookFB2(book)
	if err != nil {
		return "", func() {}, err
	}
	tmp, err := os.CreateTemp("", "ttsbook-*.fb2")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.Remove(tmp.Name()) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return "", func() {}, err
	}
	tmp.Close()
	return tmp.Name(), cleanup, nil
}

// readBookFB2 returns the book's FB2 bytes, mirroring the server's parseBookPath/readFromArchive:
// book.Path may embed a .zip/.7z archive segment (books live inside per-id-range archives) and
// book.Filename is the entry. Read in-process with the same libs the server uses (archive/zip,
// bodgit/sevenzip) — no external unzip/7z and no path-as-directory guessing.
func readBookFB2(book *database.Book) ([]byte, error) {
	root := cfg.Library.Root
	parts := strings.Split(book.Path, string(filepath.Separator))
	for i, part := range parts {
		isZip := strings.HasSuffix(strings.ToLower(part), ".zip")
		is7z := strings.HasSuffix(strings.ToLower(part), ".7z")
		if !isZip && !is7z {
			continue
		}
		archivePath := filepath.Join(root, filepath.Join(parts[:i+1]...))
		internal := book.Filename
		if i+1 < len(parts) {
			internal = filepath.Join(append(append([]string{}, parts[i+1:]...), book.Filename)...)
		}
		if isZip {
			return readZipEntry(archivePath, internal, book.Filename)
		}
		return read7zEntry(archivePath, internal, book.Filename)
	}
	return os.ReadFile(filepath.Join(root, book.Path, book.Filename)) // plain file on disk
}

func readZipEntry(archivePath, internal, filename string) ([]byte, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == internal || f.Name == filename {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not found in %s", filename, archivePath)
}

func read7zEntry(archivePath, internal, filename string) ([]byte, error) {
	sz, err := sevenzip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer sz.Close()
	for _, f := range sz.File {
		if f.Name == internal || f.Name == filename {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not found in %s", filename, archivePath)
}

// f5Env builds the environment fb2-to-f5.sh reads (see its header). Native engine, per-language voice.
func f5Env(wc config.WorkerConfig, lc config.WorkerLangConfig, mode string) []string {
	env := []string{
		"ENGINE=native",
		"MODE=" + mode,
		"MAXCHARS=" + strconv.Itoa(wc.MaxChars),
		"F5_HOME=" + expandHome(wc.F5Home),
		"F5MODEL=" + expandHome(lc.F5Model),
	}
	if lc.NotesModel != "" {
		env = append(env, "F5MODEL_NOTES="+expandHome(lc.NotesModel))
	}
	if wc.F5Bin != "" {
		env = append(env, "F5BIN="+expandHome(wc.F5Bin))
	}
	if wc.NFE > 0 { // else fb2-to-f5.sh's default (16) applies
		env = append(env, "NFE="+strconv.Itoa(wc.NFE))
	}
	if wc.Combine > 0 { // else fb2-to-f5.sh's default (1 = one MP3 per top-level section)
		env = append(env, "COMBINE="+strconv.Itoa(wc.Combine))
	}
	if exe, err := os.Executable(); err == nil { // fb2-to-f5.sh calls `$SOPDS fb2-extract …`
		env = append(env, "SOPDS="+exe)
	}
	// How to accent before synth: "ruaccent" (ru, default) · "none" (en — F5 English reads plain text) ·
	// "uk-stress" (uk, later). fb2-to-f5.sh branches on STRESS_MODE.
	stress := lc.Stress
	if stress == "" {
		stress = "ruaccent"
	}
	env = append(env, "STRESS_MODE="+stress)
	return env
}

// packageAudiobook arranges outDir/*.mp3 as "<title>/NNN - *.mp3" and 7z's it into destDir, matching
// the folder-per-audiobook layout the scanner groups (like the library's existing StephenKing_Joyland.7z).
func packageAudiobook(outDir, title, destDir string) (string, error) {
	mp3s, _ := filepath.Glob(filepath.Join(outDir, "*.mp3"))
	if len(mp3s) == 0 {
		return "", fmt.Errorf("no mp3 produced in %s", outDir)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	folder := sanitize(title)
	stageParent := filepath.Join(outDir, "_pack")
	stage := filepath.Join(stageParent, folder)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return "", err
	}
	for i, m := range mp3s { // fb2-to-f5.sh names them NN_title.mp3 → Glob returns them sorted
		dst := filepath.Join(stage, fmt.Sprintf("%03d - %s.mp3", i+1, folder))
		if err := os.Rename(m, dst); err != nil {
			return "", err
		}
	}
	archive := filepath.Join(destDir, folder+".7z")
	_ = os.Remove(archive)
	// -mx=1: audio is already compressed, so store fast; archive contains the single top folder.
	c := exec.Command("7zz", "a", "-mx=1", archive, stage)
	if out, err := c.CombinedOutput(); err != nil {
		return "", fmt.Errorf("7zz: %v: %s", err, out)
	}
	return archive, nil
}

var sanitizeRe = regexp.MustCompile(`[^\p{L}\p{N} .\-]+`)

func sanitize(s string) string {
	s = sanitizeRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if s == "" {
		s = "audiobook"
	}
	return s
}

// normLang maps a book language to the config key (base 2-letter code, lowercased).
func normLang(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	if len(l) >= 2 {
		return l[:2]
	}
	return l
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}
