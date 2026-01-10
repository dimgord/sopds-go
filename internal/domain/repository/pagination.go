package repository

// Pagination contains pagination parameters and results
type Pagination struct {
	// Input parameters
	Page     int // 0-based page number
	PageSize int // Items per page (0 = no limit)

	// Output (populated by repository)
	TotalCount int64 // Total number of items matching filters
	HasNext    bool  // True if there are more pages
	HasPrev    bool  // True if there are previous pages
}

// NewPagination creates pagination with defaults
func NewPagination(page, pageSize int) *Pagination {
	if page < 0 {
		page = 0
	}
	if pageSize < 0 {
		pageSize = 50
	}
	return &Pagination{
		Page:     page,
		PageSize: pageSize,
	}
}

// DefaultPagination returns default pagination (page 0, 50 items)
func DefaultPagination() *Pagination {
	return NewPagination(0, 50)
}

// NoPagination returns pagination with no limit
func NoPagination() *Pagination {
	return &Pagination{
		Page:     0,
		PageSize: 0, // No limit
	}
}

// Offset calculates the SQL offset
func (p *Pagination) Offset() int {
	return p.Page * p.PageSize
}

// Limit returns the page size (0 means no limit)
func (p *Pagination) Limit() int {
	return p.PageSize
}

// SetResults sets the output fields based on total count
func (p *Pagination) SetResults(totalCount int64) {
	p.TotalCount = totalCount
	p.HasPrev = p.Page > 0
	if p.PageSize > 0 {
		p.HasNext = int64(p.Offset()+p.PageSize) < totalCount
	} else {
		p.HasNext = false
	}
}

// TotalPages returns the total number of pages
func (p *Pagination) TotalPages() int {
	if p.PageSize <= 0 || p.TotalCount == 0 {
		return 1
	}
	pages := int(p.TotalCount) / p.PageSize
	if int(p.TotalCount)%p.PageSize > 0 {
		pages++
	}
	return pages
}

// IsLastPage returns true if this is the last page
func (p *Pagination) IsLastPage() bool {
	return !p.HasNext
}

// NextPage returns pagination for the next page
func (p *Pagination) NextPage() *Pagination {
	return NewPagination(p.Page+1, p.PageSize)
}

// PrevPage returns pagination for the previous page
func (p *Pagination) PrevPage() *Pagination {
	page := p.Page - 1
	if page < 0 {
		page = 0
	}
	return NewPagination(page, p.PageSize)
}

// Query combines filters, sorting, and pagination for a query
type Query struct {
	Filters    interface{} // *BookFilters, *AuthorFilters, etc.
	Sort       Sort
	Pagination *Pagination
}

// NewQuery creates a new query with defaults
func NewQuery() *Query {
	return &Query{
		Sort:       DefaultSort(),
		Pagination: DefaultPagination(),
	}
}

// WithFilters sets the filters
func (q *Query) WithFilters(filters interface{}) *Query {
	q.Filters = filters
	return q
}

// WithSort sets the sorting
func (q *Query) WithSort(sort Sort) *Query {
	q.Sort = sort
	return q
}

// WithPagination sets the pagination
func (q *Query) WithPagination(pagination *Pagination) *Query {
	q.Pagination = pagination
	return q
}

// BookQuery is a typed query for books
type BookQuery struct {
	Filters    *BookFilters
	Sort       Sort
	Pagination *Pagination
}

// NewBookQuery creates a new book query with defaults
func NewBookQuery() *BookQuery {
	return &BookQuery{
		Filters:    NewBookFilters(),
		Sort:       DefaultSort(),
		Pagination: DefaultPagination(),
	}
}

// AuthorQuery is a typed query for authors
type AuthorQuery struct {
	Filters    *AuthorFilters
	Sort       Sort
	Pagination *Pagination
}

// NewAuthorQuery creates a new author query with defaults
func NewAuthorQuery() *AuthorQuery {
	return &AuthorQuery{
		Filters:    NewAuthorFilters(),
		Sort:       ByName(),
		Pagination: DefaultPagination(),
	}
}

// SeriesQuery is a typed query for series
type SeriesQuery struct {
	Filters    *SeriesFilters
	Sort       Sort
	Pagination *Pagination
}

// NewSeriesQuery creates a new series query with defaults
func NewSeriesQuery() *SeriesQuery {
	return &SeriesQuery{
		Filters:    NewSeriesFilters(),
		Sort:       ByName(),
		Pagination: DefaultPagination(),
	}
}

// GenreQuery is a typed query for genres
type GenreQuery struct {
	Filters    *GenreFilters
	Sort       Sort
	Pagination *Pagination
}

// NewGenreQuery creates a new genre query with defaults
func NewGenreQuery() *GenreQuery {
	return &GenreQuery{
		Filters:    NewGenreFilters(),
		Sort:       ByName(),
		Pagination: DefaultPagination(),
	}
}

// CatalogQuery is a typed query for catalogs
type CatalogQuery struct {
	Filters    *CatalogFilters
	Sort       Sort
	Pagination *Pagination
}

// NewCatalogQuery creates a new catalog query with defaults
func NewCatalogQuery() *CatalogQuery {
	return &CatalogQuery{
		Filters:    NewCatalogFilters(),
		Sort:       ByName(),
		Pagination: DefaultPagination(),
	}
}
