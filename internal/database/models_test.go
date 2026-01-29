package database

import (
	"testing"
)

func TestAuthorFullName(t *testing.T) {
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
		a := &Author{FirstName: tc.firstName, LastName: tc.lastName}
		result := a.FullName()
		if result != tc.expected {
			t.Errorf("Author{%q, %q}.FullName() = %q, expected %q",
				tc.firstName, tc.lastName, result, tc.expected)
		}
	}
}

func TestNewPagination(t *testing.T) {
	tests := []struct {
		page          int
		limit         int
		expectedPage  int
		expectedLimit int
	}{
		{0, 50, 0, 50},
		{1, 50, 1, 50},
		{-1, 50, 0, 50},   // Negative page becomes 0
		{0, 0, 0, 50},     // Zero limit becomes 50
		{0, -10, 0, 50},   // Negative limit becomes 50
		{5, 100, 5, 100},
	}

	for _, tc := range tests {
		p := NewPagination(tc.page, tc.limit)
		if p.Page != tc.expectedPage {
			t.Errorf("NewPagination(%d, %d).Page = %d, expected %d",
				tc.page, tc.limit, p.Page, tc.expectedPage)
		}
		if p.Limit != tc.expectedLimit {
			t.Errorf("NewPagination(%d, %d).Limit = %d, expected %d",
				tc.page, tc.limit, p.Limit, tc.expectedLimit)
		}
	}
}

func TestPaginationOffset(t *testing.T) {
	tests := []struct {
		page     int
		limit    int
		expected int
	}{
		{0, 50, 0},
		{1, 50, 50},
		{2, 50, 100},
		{0, 100, 0},
		{3, 25, 75},
	}

	for _, tc := range tests {
		p := NewPagination(tc.page, tc.limit)
		result := p.Offset()
		if result != tc.expected {
			t.Errorf("Pagination{Page: %d, Limit: %d}.Offset() = %d, expected %d",
				tc.page, tc.limit, result, tc.expected)
		}
	}
}

func TestCatTypeConstants(t *testing.T) {
	if CatNormal != 0 {
		t.Errorf("CatNormal = %d, expected 0", CatNormal)
	}
	if CatZip != 1 {
		t.Errorf("CatZip = %d, expected 1", CatZip)
	}
	if CatGz != 2 {
		t.Errorf("CatGz = %d, expected 2", CatGz)
	}
}

func TestAvailConstants(t *testing.T) {
	if AvailDeleted != 0 {
		t.Errorf("AvailDeleted = %d, expected 0", AvailDeleted)
	}
	if AvailPending != 1 {
		t.Errorf("AvailPending = %d, expected 1", AvailPending)
	}
	if AvailVerified != 2 {
		t.Errorf("AvailVerified = %d, expected 2", AvailVerified)
	}
}

func TestDuplicateModeConstants(t *testing.T) {
	if DupNone != 0 {
		t.Errorf("DupNone = %d, expected 0", DupNone)
	}
	if DupNormal != 1 {
		t.Errorf("DupNormal = %d, expected 1", DupNormal)
	}
	if DupStrong != 2 {
		t.Errorf("DupStrong = %d, expected 2", DupStrong)
	}
	if DupClear != 3 {
		t.Errorf("DupClear = %d, expected 3", DupClear)
	}
}

func TestBookStruct(t *testing.T) {
	b := Book{
		ID:       1,
		Filename: "test.fb2",
		Path:     "/books",
		Format:   "fb2",
		Filesize: 1024,
		Title:    "Test Book",
	}

	if b.ID != 1 {
		t.Errorf("Book.ID = %d, expected 1", b.ID)
	}
	if b.Filename != "test.fb2" {
		t.Errorf("Book.Filename = %s, expected test.fb2", b.Filename)
	}
}

func TestGenreStruct(t *testing.T) {
	g := Genre{
		ID:         1,
		Genre:      "fiction",
		Section:    "Fiction",
		Subsection: "Science Fiction",
	}

	if g.ID != 1 {
		t.Errorf("Genre.ID = %d, expected 1", g.ID)
	}
	if g.Section != "Fiction" {
		t.Errorf("Genre.Section = %s, expected Fiction", g.Section)
	}
}

func TestSeriesStruct(t *testing.T) {
	s := Series{
		ID:   1,
		Name: "Foundation",
	}

	if s.ID != 1 {
		t.Errorf("Series.ID = %d, expected 1", s.ID)
	}
	if s.Name != "Foundation" {
		t.Errorf("Series.Name = %s, expected Foundation", s.Name)
	}
}

func TestBookSeriesStruct(t *testing.T) {
	bs := BookSeries{
		SeriesID: 1,
		BookID:   2,
		SerNo:    3,
		Name:     "Foundation",
	}

	if bs.SeriesID != 1 {
		t.Errorf("BookSeries.SeriesID = %d, expected 1", bs.SeriesID)
	}
	if bs.SerNo != 3 {
		t.Errorf("BookSeries.SerNo = %d, expected 3", bs.SerNo)
	}
}

func TestCatalogStruct(t *testing.T) {
	parentID := int64(1)
	c := Catalog{
		ID:       2,
		ParentID: &parentID,
		Name:     "Subfolder",
		Path:     "/books/subfolder",
		CatType:  CatNormal,
	}

	if c.ID != 2 {
		t.Errorf("Catalog.ID = %d, expected 2", c.ID)
	}
	if *c.ParentID != 1 {
		t.Errorf("Catalog.ParentID = %d, expected 1", *c.ParentID)
	}
}

func TestCatalogItemStruct(t *testing.T) {
	item := CatalogItem{
		ItemType: "book",
		ID:       1,
		Name:     "test.fb2",
		Title:    "Test Book",
		Format:   "fb2",
		Filesize: 1024,
	}

	if item.ItemType != "book" {
		t.Errorf("CatalogItem.ItemType = %s, expected book", item.ItemType)
	}
}
