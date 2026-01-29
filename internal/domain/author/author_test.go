package author

import (
	"testing"
)

func TestNew(t *testing.T) {
	a := New("John", "Doe")
	if a.FirstName() != "John" {
		t.Errorf("FirstName() = %q, expected John", a.FirstName())
	}
	if a.LastName() != "Doe" {
		t.Errorf("LastName() = %q, expected Doe", a.LastName())
	}
}

func TestNewTrimsWhitespace(t *testing.T) {
	a := New("  John  ", "  Doe  ")
	if a.FirstName() != "John" {
		t.Errorf("FirstName() = %q, expected John (trimmed)", a.FirstName())
	}
	if a.LastName() != "Doe" {
		t.Errorf("LastName() = %q, expected Doe (trimmed)", a.LastName())
	}
}

func TestReconstruct(t *testing.T) {
	a := Reconstruct(42, "John", "Doe")
	if a.ID() != 42 {
		t.Errorf("ID() = %d, expected 42", a.ID())
	}
	if a.FirstName() != "John" {
		t.Errorf("FirstName() = %q, expected John", a.FirstName())
	}
}

func TestFullName(t *testing.T) {
	tests := []struct {
		firstName string
		lastName  string
		expected  string
	}{
		{"John", "Doe", "Doe John"},
		{"", "Doe", "Doe"},
		{"John", "", "John"},
		{"", "", ""},
		{"Mary Jane", "Watson", "Watson Mary Jane"},
	}

	for _, tc := range tests {
		a := New(tc.firstName, tc.lastName)
		result := a.FullName()
		if result != tc.expected {
			t.Errorf("New(%q, %q).FullName() = %q, expected %q",
				tc.firstName, tc.lastName, result, tc.expected)
		}
	}
}

func TestDisplayName(t *testing.T) {
	tests := []struct {
		firstName string
		lastName  string
		expected  string
	}{
		{"John", "Doe", "John Doe"},
		{"", "Doe", "Doe"},
		{"John", "", "John"},
		{"", "", ""},
	}

	for _, tc := range tests {
		a := New(tc.firstName, tc.lastName)
		result := a.DisplayName()
		if result != tc.expected {
			t.Errorf("New(%q, %q).DisplayName() = %q, expected %q",
				tc.firstName, tc.lastName, result, tc.expected)
		}
	}
}

func TestSortKey(t *testing.T) {
	a := New("John", "Doe")
	expected := "doe john"
	if a.SortKey() != expected {
		t.Errorf("SortKey() = %q, expected %q", a.SortKey(), expected)
	}
}

func TestIsUnknown(t *testing.T) {
	tests := []struct {
		firstName string
		lastName  string
		expected  bool
	}{
		{"", "", true},
		{"John", "", false},
		{"", "Doe", false},
		{"John", "Doe", false},
	}

	for _, tc := range tests {
		a := New(tc.firstName, tc.lastName)
		result := a.IsUnknown()
		if result != tc.expected {
			t.Errorf("New(%q, %q).IsUnknown() = %v, expected %v",
				tc.firstName, tc.lastName, result, tc.expected)
		}
	}
}

func TestMatches(t *testing.T) {
	a := New("John", "Doe")

	tests := []struct {
		firstName string
		lastName  string
		expected  bool
	}{
		{"John", "Doe", true},
		{"john", "doe", true},   // Case insensitive
		{"JOHN", "DOE", true},   // Case insensitive
		{"Jane", "Doe", false},
		{"John", "Smith", false},
	}

	for _, tc := range tests {
		result := a.Matches(tc.firstName, tc.lastName)
		if result != tc.expected {
			t.Errorf("Matches(%q, %q) = %v, expected %v",
				tc.firstName, tc.lastName, result, tc.expected)
		}
	}
}

func TestSetID(t *testing.T) {
	a := New("John", "Doe")
	if a.ID() != 0 {
		t.Errorf("Initial ID() = %d, expected 0", a.ID())
	}

	a.SetID(42)
	if a.ID() != 42 {
		t.Errorf("After SetID(42), ID() = %d, expected 42", a.ID())
	}
}
