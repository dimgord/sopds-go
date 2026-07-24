package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/dimgord/sopds-go/internal/narrate"
	"github.com/spf13/cobra"
)

// runFB2Extract is the native replacement for the old xmllint/awk extraction and fb2_extract.py:
// it writes NN_<safe>.raw.txt (chunked narration) + _titles.tsv into <review_dir>, which fb2-to-f5.sh
// then stresses + synthesizes. The section map is printed to stderr.
func runFB2Extract(cmd *cobra.Command, args []string) error {
	maxchars, err := strconv.Atoi(args[2])
	if err != nil || maxchars <= 0 {
		return fmt.Errorf("maxchars must be a positive integer, got %q", args[2])
	}
	selector := ""
	if len(args) > 3 {
		selector = args[3]
	}
	combine, _ := cmd.Flags().GetInt("combine")
	notePrefix, _ := cmd.Flags().GetString("note-prefix")

	mapLines, err := narrate.Extract(args[0], args[1], maxchars, selector, combine, notePrefix)
	for _, l := range mapLines {
		fmt.Fprintln(os.Stderr, l)
	}
	return err
}
