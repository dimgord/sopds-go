package repository

import (
	"context"

	"github.com/sopds/sopds-go/internal/domain/series"
)

// SeriesWithCount represents a series with book count
type SeriesWithCount struct {
	Series    *series.Series
	BookCount int64
}

// SeriesRepository defines the interface for series persistence
type SeriesRepository interface {
	// --- CRUD Operations ---

	// FindByID retrieves a series by ID
	FindByID(ctx context.Context, id series.ID) (*series.Series, error)

	// FindByName finds a series by its name
	FindByName(ctx context.Context, name string) (*series.Series, error)

	// Save persists a series (insert or update)
	Save(ctx context.Context, s *series.Series) error

	// GetOrCreate finds an existing series or creates a new one
	GetOrCreate(ctx context.Context, name string) (*series.Series, error)

	// Delete removes a series by ID
	Delete(ctx context.Context, id series.ID) error

	// --- Query Operations ---

	// Find returns series matching the query (filters + sort + pagination)
	Find(ctx context.Context, query *SeriesQuery) ([]*series.Series, error)

	// FindWithCounts returns series with their book counts
	FindWithCounts(ctx context.Context, query *SeriesQuery) ([]SeriesWithCount, error)

	// Count returns the total count of series matching filters
	Count(ctx context.Context, filters *SeriesFilters) (int64, error)

	// --- Prefix Navigation ---

	// GetPrefixes returns distinct name prefixes for hierarchical navigation
	// length specifies the prefix length (1, 2, or 3 characters)
	// parentPrefix filters to prefixes starting with this string
	GetPrefixes(ctx context.Context, length int, parentPrefix string) ([]string, error)
}
