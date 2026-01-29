package series

import (
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	s, err := New("Foundation")
	if err != nil {
		t.Fatalf("New(Foundation) returned error: %v", err)
	}
	if s.Name() != "Foundation" {
		t.Errorf("Name() = %q, expected Foundation", s.Name())
	}
}

func TestNewTrimsWhitespace(t *testing.T) {
	s, err := New("  Foundation  ")
	if err != nil {
		t.Fatalf("New with whitespace returned error: %v", err)
	}
	if s.Name() != "Foundation" {
		t.Errorf("Name() = %q, expected Foundation (trimmed)", s.Name())
	}
}

func TestNewEmptyName(t *testing.T) {
	_, err := New("")
	if err == nil {
		t.Error("New('') should return error")
	}
	if !errors.Is(err, ErrEmptySeriesName) {
		t.Errorf("Expected ErrEmptySeriesName, got %v", err)
	}
}

func TestNewWhitespaceOnlyName(t *testing.T) {
	_, err := New("   ")
	if err == nil {
		t.Error("New('   ') should return error")
	}
	if !errors.Is(err, ErrEmptySeriesName) {
		t.Errorf("Expected ErrEmptySeriesName, got %v", err)
	}
}

func TestReconstruct(t *testing.T) {
	s := Reconstruct(42, "Foundation")
	if s.ID() != 42 {
		t.Errorf("ID() = %d, expected 42", s.ID())
	}
	if s.Name() != "Foundation" {
		t.Errorf("Name() = %q, expected Foundation", s.Name())
	}
}

func TestSortKey(t *testing.T) {
	s, _ := New("The Foundation")
	expected := "the foundation"
	if s.SortKey() != expected {
		t.Errorf("SortKey() = %q, expected %q", s.SortKey(), expected)
	}
}

func TestMatches(t *testing.T) {
	s, _ := New("Foundation")

	tests := []struct {
		name     string
		expected bool
	}{
		{"Foundation", true},
		{"foundation", true},   // Case insensitive
		{"FOUNDATION", true},   // Case insensitive
		{"Dune", false},
	}

	for _, tc := range tests {
		result := s.Matches(tc.name)
		if result != tc.expected {
			t.Errorf("Matches(%q) = %v, expected %v", tc.name, result, tc.expected)
		}
	}
}

func TestSetID(t *testing.T) {
	s, _ := New("Foundation")
	if s.ID() != 0 {
		t.Errorf("Initial ID() = %d, expected 0", s.ID())
	}

	s.SetID(42)
	if s.ID() != 42 {
		t.Errorf("After SetID(42), ID() = %d, expected 42", s.ID())
	}
}

func TestNewBookInSeries(t *testing.T) {
	bis := NewBookInSeries(1, 2, 3)
	if bis.BookID != 1 {
		t.Errorf("BookID = %d, expected 1", bis.BookID)
	}
	if bis.SeriesID != 2 {
		t.Errorf("SeriesID = %d, expected 2", bis.SeriesID)
	}
	if bis.Number != 3 {
		t.Errorf("Number = %d, expected 3", bis.Number)
	}
}

func TestHasNumber(t *testing.T) {
	tests := []struct {
		number   int
		expected bool
	}{
		{0, false},
		{1, true},
		{5, true},
		{-1, false},
	}

	for _, tc := range tests {
		bis := NewBookInSeries(1, 1, tc.number)
		result := bis.HasNumber()
		if result != tc.expected {
			t.Errorf("BookInSeries{Number: %d}.HasNumber() = %v, expected %v",
				tc.number, result, tc.expected)
		}
	}
}
