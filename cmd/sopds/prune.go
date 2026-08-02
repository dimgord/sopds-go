package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
)

// runDeleteBook removes one book by id. Child rows (bauthors/bgenres/bseries) cascade via FK;
// audiobook tracks live in the book row itself. No undo — the id is given explicitly.
func runDeleteBook(cmd *cobra.Command, args []string) error {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid book id %q: %w", args[0], err)
	}
	svc, err := ttsService()
	if err != nil {
		return err
	}
	defer svc.Close()
	ctx := context.Background()

	b, err := svc.GetBook(ctx, id)
	if err != nil {
		return err
	}
	if b == nil {
		return fmt.Errorf("no book with book_id=%d", id)
	}
	if err := svc.DeleteBook(ctx, id); err != nil {
		return err
	}
	fmt.Printf("Deleted book_id=%d  %q  (path=%q filename=%q)\n", id, b.Title, b.Path, b.Filename)
	return nil
}

// runPrune finds audiobooks whose archive/folder no longer resolves on disk — the "path=filename"
// duplicates (path==filename ⇒ root/x.7z/x.7z, never a real file) and any other stale rows. It
// resolves the path exactly like the web serve does (getBookPath: Join(root, path, filename)), so a
// functional book (path=".") is never flagged. Dry-run by default; --delete actually removes them.
func runPrune(cmd *cobra.Command, args []string) error {
	doDelete, _ := cmd.Flags().GetBool("delete")
	svc, err := ttsService()
	if err != nil {
		return err
	}
	defer svc.Close()
	ctx := context.Background()

	rows, err := svc.ListAudiobooks(ctx, 0) // 0 ⇒ all
	if err != nil {
		return err
	}
	root := cfg.Library.Root

	type stale struct {
		id       int64
		title    string
		resolved string
	}
	var missing []stale
	for _, r := range rows {
		b, err := svc.GetBook(ctx, r.BookID)
		if err != nil {
			return err
		}
		if b == nil {
			continue
		}
		p := filepath.Join(root, b.Path, b.Filename)
		if _, err := os.Stat(p); err != nil {
			missing = append(missing, stale{r.BookID, r.Title, p})
		}
	}

	if len(missing) == 0 {
		fmt.Printf("Scanned %d audiobooks under %s — all files present, nothing to prune.\n", len(rows), root)
		return nil
	}

	fmt.Printf("Stale audiobooks (resolved file missing) — %d of %d:\n", len(missing), len(rows))
	fmt.Printf("%-8s  %s\n", "book_id", "resolved (missing) path  |  title")
	for _, m := range missing {
		fmt.Printf("%-8d  %s  |  %s\n", m.id, m.resolved, m.title)
	}
	if !doDelete {
		fmt.Printf("\nDry run. Re-run with --delete to remove these %d row(s) (children cascade).\n", len(missing))
		return nil
	}
	for _, m := range missing {
		if err := svc.DeleteBook(ctx, m.id); err != nil {
			return fmt.Errorf("delete book_id=%d: %w", m.id, err)
		}
	}
	fmt.Printf("\nDeleted %d stale audiobook row(s).\n", len(missing))
	return nil
}
