package repository

import (
	"testing"
)

// --- Pagination Tests ---

func TestNewPagination(t *testing.T) {
	tests := []struct {
		page     int
		pageSize int
		expPage  int
		expSize  int
	}{
		{0, 50, 0, 50},
		{1, 50, 1, 50},
		{-1, 50, 0, 50},  // Negative page becomes 0
		{0, -10, 0, 50},  // Negative size becomes 50
		{5, 100, 5, 100},
	}

	for _, tc := range tests {
		p := NewPagination(tc.page, tc.pageSize)
		if p.Page != tc.expPage {
			t.Errorf("NewPagination(%d, %d).Page = %d, expected %d",
				tc.page, tc.pageSize, p.Page, tc.expPage)
		}
		if p.PageSize != tc.expSize {
			t.Errorf("NewPagination(%d, %d).PageSize = %d, expected %d",
				tc.page, tc.pageSize, p.PageSize, tc.expSize)
		}
	}
}

func TestDefaultPagination(t *testing.T) {
	p := DefaultPagination()
	if p.Page != 0 {
		t.Errorf("DefaultPagination().Page = %d, expected 0", p.Page)
	}
	if p.PageSize != 50 {
		t.Errorf("DefaultPagination().PageSize = %d, expected 50", p.PageSize)
	}
}

func TestNoPagination(t *testing.T) {
	p := NoPagination()
	if p.PageSize != 0 {
		t.Errorf("NoPagination().PageSize = %d, expected 0", p.PageSize)
	}
}

func TestPaginationOffset(t *testing.T) {
	tests := []struct {
		page     int
		pageSize int
		expected int
	}{
		{0, 50, 0},
		{1, 50, 50},
		{2, 50, 100},
		{3, 25, 75},
	}

	for _, tc := range tests {
		p := NewPagination(tc.page, tc.pageSize)
		result := p.Offset()
		if result != tc.expected {
			t.Errorf("Pagination{%d, %d}.Offset() = %d, expected %d",
				tc.page, tc.pageSize, result, tc.expected)
		}
	}
}

func TestPaginationLimit(t *testing.T) {
	p := NewPagination(0, 100)
	if p.Limit() != 100 {
		t.Errorf("Limit() = %d, expected 100", p.Limit())
	}
}

func TestPaginationSetResults(t *testing.T) {
	p := NewPagination(1, 50)
	p.SetResults(200)

	if p.TotalCount != 200 {
		t.Errorf("TotalCount = %d, expected 200", p.TotalCount)
	}
	if !p.HasPrev {
		t.Error("HasPrev should be true for page 1")
	}
	if !p.HasNext {
		t.Error("HasNext should be true when more items exist")
	}
}

func TestPaginationSetResultsFirstPage(t *testing.T) {
	p := NewPagination(0, 50)
	p.SetResults(100)

	if p.HasPrev {
		t.Error("HasPrev should be false for page 0")
	}
	if !p.HasNext {
		t.Error("HasNext should be true when more items exist")
	}
}

func TestPaginationSetResultsLastPage(t *testing.T) {
	p := NewPagination(1, 50)
	p.SetResults(100)

	if p.HasNext {
		t.Error("HasNext should be false on last page")
	}
}

func TestPaginationTotalPages(t *testing.T) {
	tests := []struct {
		pageSize   int
		totalCount int64
		expected   int
	}{
		{50, 100, 2},
		{50, 101, 3},
		{50, 50, 1},
		{50, 0, 1},
		{0, 100, 1}, // No limit
	}

	for _, tc := range tests {
		p := NewPagination(0, tc.pageSize)
		p.TotalCount = tc.totalCount
		result := p.TotalPages()
		if result != tc.expected {
			t.Errorf("TotalPages() with size=%d, count=%d = %d, expected %d",
				tc.pageSize, tc.totalCount, result, tc.expected)
		}
	}
}

func TestPaginationIsLastPage(t *testing.T) {
	p := NewPagination(0, 50)
	p.SetResults(50)

	if !p.IsLastPage() {
		t.Error("IsLastPage() should be true when no more items")
	}

	p.SetResults(100)
	if p.IsLastPage() {
		t.Error("IsLastPage() should be false when more items exist")
	}
}

func TestPaginationNextPage(t *testing.T) {
	p := NewPagination(2, 50)
	next := p.NextPage()

	if next.Page != 3 {
		t.Errorf("NextPage().Page = %d, expected 3", next.Page)
	}
	if next.PageSize != 50 {
		t.Errorf("NextPage().PageSize = %d, expected 50", next.PageSize)
	}
}

func TestPaginationPrevPage(t *testing.T) {
	p := NewPagination(2, 50)
	prev := p.PrevPage()

	if prev.Page != 1 {
		t.Errorf("PrevPage().Page = %d, expected 1", prev.Page)
	}
}

func TestPaginationPrevPageBoundary(t *testing.T) {
	p := NewPagination(0, 50)
	prev := p.PrevPage()

	if prev.Page != 0 {
		t.Errorf("PrevPage() from 0 should stay at 0, got %d", prev.Page)
	}
}

// --- Sort Tests ---

func TestNewSort(t *testing.T) {
	s := NewSort(SortByTitle, SortAsc)
	if s.Field != SortByTitle {
		t.Errorf("Field = %q, expected %q", s.Field, SortByTitle)
	}
	if s.Direction != SortAsc {
		t.Errorf("Direction = %q, expected %q", s.Direction, SortAsc)
	}
}

func TestNewSortDefaults(t *testing.T) {
	s := NewSort("", "")
	if s.Field != SortByTitle {
		t.Errorf("Default Field = %q, expected %q", s.Field, SortByTitle)
	}
	if s.Direction != SortAsc {
		t.Errorf("Default Direction = %q, expected %q", s.Direction, SortAsc)
	}
}

func TestDefaultSort(t *testing.T) {
	s := DefaultSort()
	if s.Field != SortByTitle || s.Direction != SortAsc {
		t.Errorf("DefaultSort() = {%q, %q}, expected {title, asc}",
			s.Field, s.Direction)
	}
}

func TestByDateDesc(t *testing.T) {
	s := ByDateDesc()
	if s.Field != SortByDate || s.Direction != SortDesc {
		t.Errorf("ByDateDesc() = {%q, %q}, expected {date, desc}",
			s.Field, s.Direction)
	}
}

func TestBySizeDesc(t *testing.T) {
	s := BySizeDesc()
	if s.Field != SortBySize || s.Direction != SortDesc {
		t.Errorf("BySizeDesc() = {%q, %q}, expected {size, desc}",
			s.Field, s.Direction)
	}
}

func TestByName(t *testing.T) {
	s := ByName()
	if s.Field != SortByName || s.Direction != SortAsc {
		t.Errorf("ByName() = {%q, %q}, expected {name, asc}",
			s.Field, s.Direction)
	}
}

func TestSortIsDescending(t *testing.T) {
	asc := NewSort(SortByTitle, SortAsc)
	if asc.IsDescending() {
		t.Error("SortAsc.IsDescending() should be false")
	}

	desc := NewSort(SortByTitle, SortDesc)
	if !desc.IsDescending() {
		t.Error("SortDesc.IsDescending() should be true")
	}
}

// --- BookFilters Tests ---

func TestNewBookFilters(t *testing.T) {
	f := NewBookFilters()
	if f == nil {
		t.Fatal("NewBookFilters() returned nil")
	}
	if f.ShowDuplicates {
		t.Error("ShowDuplicates should be false by default")
	}
	if f.ShowDeleted {
		t.Error("ShowDeleted should be false by default")
	}
	if f.FavoritesOnly {
		t.Error("FavoritesOnly should be false by default")
	}
}

func TestBookFiltersWithKeywords(t *testing.T) {
	f := NewBookFilters().WithKeywords("test", "query")
	if len(f.Keywords) != 2 {
		t.Errorf("Keywords count = %d, expected 2", len(f.Keywords))
	}
	if f.Keywords[0] != "test" || f.Keywords[1] != "query" {
		t.Error("Keywords not set correctly")
	}
}

func TestBookFiltersIncludeAnnotation(t *testing.T) {
	f := NewBookFilters().IncludeAnnotation()
	if !f.SearchInAnnotation {
		t.Error("SearchInAnnotation should be true")
	}
}

func TestBookFiltersWithAuthorName(t *testing.T) {
	f := NewBookFilters().WithAuthorName("John Doe")
	if f.AuthorNameQuery != "John Doe" {
		t.Errorf("AuthorNameQuery = %q, expected John Doe", f.AuthorNameQuery)
	}
}

func TestBookFiltersWithPatterns(t *testing.T) {
	f := NewBookFilters().
		WithLangPattern("uk").
		WithGenrePattern("comedy").
		WithSeriesPattern("Silo")

	if f.LangPattern != "uk" {
		t.Errorf("LangPattern = %q, expected uk", f.LangPattern)
	}
	if f.GenrePattern != "comedy" {
		t.Errorf("GenrePattern = %q, expected comedy", f.GenrePattern)
	}
	if f.SeriesPattern != "Silo" {
		t.Errorf("SeriesPattern = %q, expected Silo", f.SeriesPattern)
	}
}

func TestBookFiltersWithAuthors(t *testing.T) {
	f := NewBookFilters().WithAuthors(1, 2, 3)
	if len(f.AuthorIDs) != 3 {
		t.Errorf("AuthorIDs count = %d, expected 3", len(f.AuthorIDs))
	}
}

func TestBookFiltersWithGenres(t *testing.T) {
	f := NewBookFilters().WithGenres(1, 2)
	if len(f.GenreIDs) != 2 {
		t.Errorf("GenreIDs count = %d, expected 2", len(f.GenreIDs))
	}
}

func TestBookFiltersWithSeries(t *testing.T) {
	f := NewBookFilters().WithSeries(1)
	if len(f.SeriesIDs) != 1 {
		t.Errorf("SeriesIDs count = %d, expected 1", len(f.SeriesIDs))
	}
}

func TestBookFiltersWithCatalogs(t *testing.T) {
	f := NewBookFilters().WithCatalogs(1, 2)
	if len(f.CatalogIDs) != 2 {
		t.Errorf("CatalogIDs count = %d, expected 2", len(f.CatalogIDs))
	}
}

func TestBookFiltersWithSizeRange(t *testing.T) {
	min := int64(1000)
	max := int64(100000)
	f := NewBookFilters().WithSizeRange(&min, &max)

	if *f.MinSize != 1000 {
		t.Errorf("MinSize = %d, expected 1000", *f.MinSize)
	}
	if *f.MaxSize != 100000 {
		t.Errorf("MaxSize = %d, expected 100000", *f.MaxSize)
	}
}

func TestBookFiltersWithNewPeriod(t *testing.T) {
	f := NewBookFilters().WithNewPeriod(7)
	if *f.RegisteredAfter != 7 {
		t.Errorf("RegisteredAfter = %d, expected 7", *f.RegisteredAfter)
	}
}

func TestBookFiltersWithTitlePrefix(t *testing.T) {
	f := NewBookFilters().WithTitlePrefix("A")
	if f.TitlePrefix != "A" {
		t.Errorf("TitlePrefix = %q, expected A", f.TitlePrefix)
	}
}

func TestBookFiltersIncludeDuplicates(t *testing.T) {
	f := NewBookFilters().IncludeDuplicates()
	if !f.ShowDuplicates {
		t.Error("ShowDuplicates should be true")
	}
}

func TestBookFiltersOnlyFavorites(t *testing.T) {
	f := NewBookFilters().OnlyFavorites()
	if !f.FavoritesOnly {
		t.Error("FavoritesOnly should be true")
	}
}

func TestBookFiltersWithAudioOnly(t *testing.T) {
	f := NewBookFilters().WithAudioOnly()
	if !f.AudioOnly {
		t.Error("AudioOnly should be true")
	}
}

func TestBookFiltersHasFilters(t *testing.T) {
	f := NewBookFilters()
	if f.HasFilters() {
		t.Error("Empty filters should return false")
	}

	f.WithKeywords("test")
	if !f.HasFilters() {
		t.Error("Filters with keywords should return true")
	}
}

// --- AuthorFilters Tests ---

func TestNewAuthorFilters(t *testing.T) {
	f := NewAuthorFilters()
	if !f.HasBooks {
		t.Error("HasBooks should be true by default")
	}
}

func TestAuthorFiltersWithKeywords(t *testing.T) {
	f := NewAuthorFilters().WithKeywords("John")
	if len(f.Keywords) != 1 || f.Keywords[0] != "John" {
		t.Error("Keywords not set correctly")
	}
}

func TestAuthorFiltersWithNamePrefix(t *testing.T) {
	f := NewAuthorFilters().WithNamePrefix("A")
	if f.NamePrefix != "A" {
		t.Errorf("NamePrefix = %q, expected A", f.NamePrefix)
	}
}

// --- GenreFilters Tests ---

func TestNewGenreFilters(t *testing.T) {
	f := NewGenreFilters()
	if !f.HasBooks {
		t.Error("HasBooks should be true by default")
	}
}

func TestGenreFiltersWithSection(t *testing.T) {
	f := NewGenreFilters().WithSection("Fiction")
	if f.Section != "Fiction" {
		t.Errorf("Section = %q, expected Fiction", f.Section)
	}
}

// --- SeriesFilters Tests ---

func TestNewSeriesFilters(t *testing.T) {
	f := NewSeriesFilters()
	if !f.HasBooks {
		t.Error("HasBooks should be true by default")
	}
}

func TestSeriesFiltersWithKeywords(t *testing.T) {
	f := NewSeriesFilters().WithKeywords("Foundation")
	if len(f.Keywords) != 1 || f.Keywords[0] != "Foundation" {
		t.Error("Keywords not set correctly")
	}
}

func TestSeriesFiltersWithNamePrefix(t *testing.T) {
	f := NewSeriesFilters().WithNamePrefix("F")
	if f.NamePrefix != "F" {
		t.Errorf("NamePrefix = %q, expected F", f.NamePrefix)
	}
}

// --- CatalogFilters Tests ---

func TestNewCatalogFilters(t *testing.T) {
	f := NewCatalogFilters()
	if f.ParentID != nil {
		t.Error("ParentID should be nil by default")
	}
	if f.TypeZip != nil {
		t.Error("TypeZip should be nil by default")
	}
}

func TestCatalogFiltersWithParent(t *testing.T) {
	f := NewCatalogFilters().WithParent(42)
	if *f.ParentID != 42 {
		t.Errorf("ParentID = %d, expected 42", *f.ParentID)
	}
}

func TestCatalogFiltersOnlyZip(t *testing.T) {
	f := NewCatalogFilters().OnlyZip()
	if f.TypeZip == nil || !*f.TypeZip {
		t.Error("TypeZip should be true")
	}
}

func TestCatalogFiltersOnlyDirectories(t *testing.T) {
	f := NewCatalogFilters().OnlyDirectories()
	if f.TypeZip == nil || *f.TypeZip {
		t.Error("TypeZip should be false")
	}
}

// --- Query Tests ---

func TestNewQuery(t *testing.T) {
	q := NewQuery()
	if q.Pagination == nil {
		t.Error("Query.Pagination should not be nil")
	}
}

func TestQueryWithFilters(t *testing.T) {
	filters := NewBookFilters()
	q := NewQuery().WithFilters(filters)
	if q.Filters != filters {
		t.Error("Filters not set correctly")
	}
}

func TestQueryWithSort(t *testing.T) {
	sort := ByDateDesc()
	q := NewQuery().WithSort(sort)
	if q.Sort.Field != SortByDate {
		t.Error("Sort not set correctly")
	}
}

func TestQueryWithPagination(t *testing.T) {
	p := NewPagination(5, 100)
	q := NewQuery().WithPagination(p)
	if q.Pagination.Page != 5 {
		t.Error("Pagination not set correctly")
	}
}

// --- Typed Query Tests ---

func TestNewBookQuery(t *testing.T) {
	q := NewBookQuery()
	if q.Filters == nil {
		t.Error("BookQuery.Filters should not be nil")
	}
	if q.Pagination == nil {
		t.Error("BookQuery.Pagination should not be nil")
	}
}

func TestNewAuthorQuery(t *testing.T) {
	q := NewAuthorQuery()
	if q.Filters == nil {
		t.Error("AuthorQuery.Filters should not be nil")
	}
	if q.Sort.Field != SortByName {
		t.Errorf("AuthorQuery sort field = %q, expected name", q.Sort.Field)
	}
}

func TestNewSeriesQuery(t *testing.T) {
	q := NewSeriesQuery()
	if q.Filters == nil {
		t.Error("SeriesQuery.Filters should not be nil")
	}
}

func TestNewGenreQuery(t *testing.T) {
	q := NewGenreQuery()
	if q.Filters == nil {
		t.Error("GenreQuery.Filters should not be nil")
	}
}

func TestNewCatalogQuery(t *testing.T) {
	q := NewCatalogQuery()
	if q.Filters == nil {
		t.Error("CatalogQuery.Filters should not be nil")
	}
}
