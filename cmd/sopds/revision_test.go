package main

import (
	"os"
	"regexp"
	"testing"
)

// The `revision` constant is what /web/about reports, and it is the only thing that says which
// build is running: `version` stays "dev" unless GoReleaser injects a tag, so a deploy built from
// source is identified by this number alone. It is bumped by hand, and by Rev 121 it had been
// missed seven revisions running — /web/about still claimed Rev 114 on a Rev 121 binary, which
// makes the page worse than useless for verifying a deploy, because it is confidently wrong.
//
// This test makes that impossible: PROGRESS.md's newest entry is the source of truth, and the
// constant has to agree with it.
func TestRevisionMatchesProgress(t *testing.T) {
	data, err := os.ReadFile("../../PROGRESS.md")
	if err != nil {
		t.Fatalf("read PROGRESS.md: %v", err)
	}
	m := regexp.MustCompile(`(?m)^#+\s*Revision\s+(\d+)`).FindSubmatch(data)
	if m == nil {
		t.Fatal("no '### Revision N' heading found in PROGRESS.md")
	}
	if got, want := revision, string(m[1]); got != want {
		t.Errorf("revision constant is %q but PROGRESS.md's newest entry is Rev %s\n"+
			"bump `revision` in cmd/sopds/main.go — /web/about reports it", got, want)
	}
}
