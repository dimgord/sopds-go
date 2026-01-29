package catalog

import (
	"testing"
)

func TestTypeString(t *testing.T) {
	tests := []struct {
		catType  Type
		expected string
	}{
		{TypeNormal, "directory"},
		{TypeZip, "zip"},
		{TypeGzip, "gzip"},
		{Type(99), "unknown"},
	}

	for _, tc := range tests {
		result := tc.catType.String()
		if result != tc.expected {
			t.Errorf("Type(%d).String() = %q, expected %q", tc.catType, result, tc.expected)
		}
	}
}

func TestTypeIsArchive(t *testing.T) {
	tests := []struct {
		catType  Type
		expected bool
	}{
		{TypeNormal, false},
		{TypeZip, true},
		{TypeGzip, true},
	}

	for _, tc := range tests {
		result := tc.catType.IsArchive()
		if result != tc.expected {
			t.Errorf("Type(%d).IsArchive() = %v, expected %v", tc.catType, result, tc.expected)
		}
	}
}

func TestNew(t *testing.T) {
	parentID := ID(1)
	c := New("books", "/path/to/books", TypeNormal, &parentID)

	if c.Name() != "books" {
		t.Errorf("Name() = %q, expected books", c.Name())
	}
	if c.Path() != "/path/to/books" {
		t.Errorf("Path() = %q, expected /path/to/books", c.Path())
	}
	if c.Type() != TypeNormal {
		t.Errorf("Type() = %d, expected %d", c.Type(), TypeNormal)
	}
	if *c.ParentID() != 1 {
		t.Errorf("ParentID() = %d, expected 1", *c.ParentID())
	}
}

func TestNewWithNilParent(t *testing.T) {
	c := New("root", "/", TypeNormal, nil)

	if c.ParentID() != nil {
		t.Errorf("ParentID() = %v, expected nil", c.ParentID())
	}
}

func TestReconstruct(t *testing.T) {
	parentID := ID(1)
	c := Reconstruct(42, &parentID, "books", "/path", TypeZip)

	if c.ID() != 42 {
		t.Errorf("ID() = %d, expected 42", c.ID())
	}
	if c.Name() != "books" {
		t.Errorf("Name() = %q, expected books", c.Name())
	}
}

func TestIsRoot(t *testing.T) {
	parentID := ID(1)

	tests := []struct {
		name     string
		parentID *ID
		expected bool
	}{
		{"nil parent", nil, true},
		{"with parent", &parentID, false},
	}

	for _, tc := range tests {
		c := New("test", "/test", TypeNormal, tc.parentID)
		result := c.IsRoot()
		if result != tc.expected {
			t.Errorf("%s: IsRoot() = %v, expected %v", tc.name, result, tc.expected)
		}
	}
}

func TestIsArchive(t *testing.T) {
	tests := []struct {
		catType  Type
		expected bool
	}{
		{TypeNormal, false},
		{TypeZip, true},
		{TypeGzip, true},
	}

	for _, tc := range tests {
		c := New("test", "/test", tc.catType, nil)
		result := c.IsArchive()
		if result != tc.expected {
			t.Errorf("Catalog{Type: %d}.IsArchive() = %v, expected %v",
				tc.catType, result, tc.expected)
		}
	}
}

func TestIsZip(t *testing.T) {
	tests := []struct {
		catType  Type
		expected bool
	}{
		{TypeNormal, false},
		{TypeZip, true},
		{TypeGzip, false},
	}

	for _, tc := range tests {
		c := New("test", "/test", tc.catType, nil)
		result := c.IsZip()
		if result != tc.expected {
			t.Errorf("Catalog{Type: %d}.IsZip() = %v, expected %v",
				tc.catType, result, tc.expected)
		}
	}
}

func TestContainsPath(t *testing.T) {
	c := New("books", "/library/books", TypeNormal, nil)

	tests := []struct {
		path     string
		expected bool
	}{
		{"/library/books/fiction", true},
		{"/library/books", true},
		{"/library/music", false},
		{"/other/path", false},
	}

	for _, tc := range tests {
		result := c.ContainsPath(tc.path)
		if result != tc.expected {
			t.Errorf("ContainsPath(%q) = %v, expected %v", tc.path, result, tc.expected)
		}
	}
}

func TestSetID(t *testing.T) {
	c := New("test", "/test", TypeNormal, nil)
	if c.ID() != 0 {
		t.Errorf("Initial ID() = %d, expected 0", c.ID())
	}

	c.SetID(42)
	if c.ID() != 42 {
		t.Errorf("After SetID(42), ID() = %d, expected 42", c.ID())
	}
}

func TestSetParentID(t *testing.T) {
	c := New("test", "/test", TypeNormal, nil)
	if c.ParentID() != nil {
		t.Errorf("Initial ParentID() should be nil")
	}

	parentID := ID(5)
	c.SetParentID(&parentID)
	if *c.ParentID() != 5 {
		t.Errorf("After SetParentID(5), ParentID() = %d, expected 5", *c.ParentID())
	}
}

func TestItemIsBook(t *testing.T) {
	tests := []struct {
		itemType string
		expected bool
	}{
		{"book", true},
		{"catalog", false},
		{"", false},
	}

	for _, tc := range tests {
		item := Item{ItemType: tc.itemType}
		result := item.IsBook()
		if result != tc.expected {
			t.Errorf("Item{ItemType: %q}.IsBook() = %v, expected %v",
				tc.itemType, result, tc.expected)
		}
	}
}

func TestItemIsCatalog(t *testing.T) {
	tests := []struct {
		itemType string
		expected bool
	}{
		{"catalog", true},
		{"book", false},
		{"", false},
	}

	for _, tc := range tests {
		item := Item{ItemType: tc.itemType}
		result := item.IsCatalog()
		if result != tc.expected {
			t.Errorf("Item{ItemType: %q}.IsCatalog() = %v, expected %v",
				tc.itemType, result, tc.expected)
		}
	}
}
