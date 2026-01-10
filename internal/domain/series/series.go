package series

import (
	"errors"
	"strings"
)

// Domain errors
var (
	ErrEmptySeriesName = errors.New("series name cannot be empty")
)

// ID is a strongly typed identifier for series
type ID int64

// Series represents a book series
type Series struct {
	id   ID
	name string
}

// New creates a new Series with validation
func New(name string) (*Series, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrEmptySeriesName
	}
	return &Series{name: name}, nil
}

// Reconstruct recreates a Series from persistence layer
func Reconstruct(id ID, name string) *Series {
	return &Series{
		id:   id,
		name: name,
	}
}

// --- Domain Methods ---

// SortKey returns a string suitable for alphabetical sorting
func (s *Series) SortKey() string {
	return strings.ToLower(s.name)
}

// Matches returns true if this series matches the given name
func (s *Series) Matches(name string) bool {
	return strings.EqualFold(s.name, name)
}

// SetID sets the series ID (used after persistence)
func (s *Series) SetID(id ID) {
	s.id = id
}

// --- Getters ---

func (s *Series) ID() ID     { return s.id }
func (s *Series) Name() string { return s.name }

// BookInSeries represents a book's position in a series
type BookInSeries struct {
	BookID   int64
	SeriesID ID
	Number   int // Book number in the series (0 if unknown)
}

// NewBookInSeries creates a new book-series relationship
func NewBookInSeries(bookID int64, seriesID ID, number int) BookInSeries {
	return BookInSeries{
		BookID:   bookID,
		SeriesID: seriesID,
		Number:   number,
	}
}

// HasNumber returns true if the book has a number in the series
func (b BookInSeries) HasNumber() bool {
	return b.Number > 0
}
