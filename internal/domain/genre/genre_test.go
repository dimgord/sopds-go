package genre

import (
	"testing"
)

func TestNew(t *testing.T) {
	g := New("sf_fantasy", "Science Fiction", "Fantasy")
	if g.Code() != "sf_fantasy" {
		t.Errorf("Code() = %q, expected sf_fantasy", g.Code())
	}
	if g.Section() != "Science Fiction" {
		t.Errorf("Section() = %q, expected Science Fiction", g.Section())
	}
	if g.Subsection() != "Fantasy" {
		t.Errorf("Subsection() = %q, expected Fantasy", g.Subsection())
	}
}

func TestReconstruct(t *testing.T) {
	g := Reconstruct(42, "sf_fantasy", "Science Fiction", "Fantasy")
	if g.ID() != 42 {
		t.Errorf("ID() = %d, expected 42", g.ID())
	}
	if g.Code() != "sf_fantasy" {
		t.Errorf("Code() = %q, expected sf_fantasy", g.Code())
	}
}

func TestDisplayName(t *testing.T) {
	tests := []struct {
		code       string
		section    string
		subsection string
		expected   string
	}{
		{"sf_fantasy", "Science Fiction", "Fantasy", "Fantasy"},
		{"sf", "Science Fiction", "", "sf"},
		{"detective", "", "Mystery", "Mystery"},
	}

	for _, tc := range tests {
		g := New(tc.code, tc.section, tc.subsection)
		result := g.DisplayName()
		if result != tc.expected {
			t.Errorf("New(%q, %q, %q).DisplayName() = %q, expected %q",
				tc.code, tc.section, tc.subsection, result, tc.expected)
		}
	}
}

func TestFullPath(t *testing.T) {
	tests := []struct {
		code       string
		section    string
		subsection string
		expected   string
	}{
		{"sf_fantasy", "Science Fiction", "Fantasy", "Science Fiction / Fantasy"},
		{"sf", "Science Fiction", "", "Science Fiction"},
		{"detective", "", "Mystery", "Mystery"},
		{"unknown", "", "", ""},
	}

	for _, tc := range tests {
		g := New(tc.code, tc.section, tc.subsection)
		result := g.FullPath()
		if result != tc.expected {
			t.Errorf("New(%q, %q, %q).FullPath() = %q, expected %q",
				tc.code, tc.section, tc.subsection, result, tc.expected)
		}
	}
}

func TestBelongsToSection(t *testing.T) {
	g := New("sf_fantasy", "Science Fiction", "Fantasy")

	if !g.BelongsToSection("Science Fiction") {
		t.Error("BelongsToSection(Science Fiction) = false, expected true")
	}
	if g.BelongsToSection("Horror") {
		t.Error("BelongsToSection(Horror) = true, expected false")
	}
}

func TestSetID(t *testing.T) {
	g := New("sf", "Science Fiction", "")
	if g.ID() != 0 {
		t.Errorf("Initial ID() = %d, expected 0", g.ID())
	}

	g.SetID(42)
	if g.ID() != 42 {
		t.Errorf("After SetID(42), ID() = %d, expected 42", g.ID())
	}
}
