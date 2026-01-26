package scanner

import (
	"testing"
)

func TestParseINX(t *testing.T) {
	ab, err := ParseINX("/home/dimgord/sopds-go/AKYNIN.inx")
	if err != nil {
		t.Fatalf("ParseINX error: %v", err)
	}

	t.Logf("Book: %s", ab.Name)
	t.Logf("Tracks: %d", len(ab.Tracks))
	t.Logf("Total Duration: %d seconds (%.1f hours)", ab.TotalDuration(), float64(ab.TotalDuration())/3600)
	t.Logf("Version: %s", ab.Version)
	t.Logf("Codec: %+v", ab.Codec)

	if ab.Name != "AKYNIN" {
		t.Errorf("Expected book name AKYNIN, got %s", ab.Name)
	}

	if len(ab.Tracks) != 46 {
		t.Errorf("Expected 46 tracks, got %d", len(ab.Tracks))
	}

	if len(ab.Tracks) > 0 {
		t.Logf("\nFirst 3 tracks:")
		for i, tr := range ab.Tracks[:min(3, len(ab.Tracks))] {
			t.Logf("  %d. %s (%d sec)", i+1, tr.Filename, tr.Duration)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
