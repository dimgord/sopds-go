package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dimgord/sopds-go/internal/infrastructure/persistence"
	"github.com/spf13/cobra"
)

// ttsService opens the database and returns a service for the on-demand-audio fulfillment commands.
func ttsService() (*persistence.Service, error) {
	gormDB, err := persistence.NewDB(&cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	return persistence.NewService(gormDB), nil
}

// runTTSRequests prints books readers have requested audio for, most-wanted first.
func runTTSRequests(cmd *cobra.Command, args []string) error {
	svc, err := ttsService()
	if err != nil {
		return err
	}
	defer svc.Close()
	pending, err := svc.PendingTTSRequests(context.Background())
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		fmt.Println("No pending audio requests.")
		return nil
	}
	fmt.Printf("Pending audio requests (%d), most-wanted first:\n", len(pending))
	for _, p := range pending {
		fmt.Printf("  [%3d req] book_id=%-7d %s\n", p.Requests, p.BookID, p.Title)
	}
	return nil
}

// runTTSLink links a text book to its generated audiobook (fulfills the request).
func runTTSLink(cmd *cobra.Command, args []string) error {
	textID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid text book id %q: %w", args[0], err)
	}
	audioID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid audio book id %q: %w", args[1], err)
	}
	svc, err := ttsService()
	if err != nil {
		return err
	}
	defer svc.Close()
	if err := svc.SetTTSAudioID(context.Background(), textID, &audioID); err != nil {
		return err
	}
	fmt.Printf("Linked text book %d → audiobook %d (Listen button now points there).\n", textID, audioID)
	return nil
}

// runTTSUnlink clears a text book's audio link, returning it to request mode.
func runTTSUnlink(cmd *cobra.Command, args []string) error {
	textID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid text book id %q: %w", args[0], err)
	}
	svc, err := ttsService()
	if err != nil {
		return err
	}
	defer svc.Close()
	if err := svc.SetTTSAudioID(context.Background(), textID, nil); err != nil {
		return err
	}
	fmt.Printf("Unlinked text book %d (back to request mode).\n", textID)
	return nil
}
