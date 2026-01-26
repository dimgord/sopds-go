package repository

import (
	"context"

	"github.com/sopds/sopds-go/internal/domain/genre"
)

// GenreWithCount represents a genre with book count
type GenreWithCount struct {
	Genre     *genre.Genre
	BookCount int64
}

// GenreRepository defines the interface for genre persistence
type GenreRepository interface {
	// --- CRUD Operations ---

	// FindByID retrieves a genre by ID
	FindByID(ctx context.Context, id genre.ID) (*genre.Genre, error)

	// FindByCode finds a genre by its code
	FindByCode(ctx context.Context, code string) (*genre.Genre, error)

	// Save persists a genre (insert or update)
	Save(ctx context.Context, g *genre.Genre) error

	// Delete removes a genre by ID
	Delete(ctx context.Context, id genre.ID) error

	// --- Query Operations ---

	// Find returns genres matching the query (filters + sort + pagination)
	Find(ctx context.Context, query *GenreQuery) ([]*genre.Genre, error)

	// FindWithCounts returns genres with their book counts
	FindWithCounts(ctx context.Context, query *GenreQuery) ([]GenreWithCount, error)

	// Count returns the total count of genres matching filters
	Count(ctx context.Context, filters *GenreFilters) (int64, error)

	// --- Section Navigation ---

	// GetSections returns all distinct genre sections
	GetSections(ctx context.Context) ([]string, error)

	// FindBySection returns all genres in a section
	FindBySection(ctx context.Context, section string, sort Sort) ([]*genre.Genre, error)
}
