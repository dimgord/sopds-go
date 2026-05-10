package repository

import (
	"context"

	"github.com/dimgord/sopds-go/internal/domain/author"
)

// AuthorWithCount represents an author with book count
type AuthorWithCount struct {
	Author    *author.Author
	BookCount int64
}

// AuthorRepository defines the interface for author persistence
type AuthorRepository interface {
	// --- CRUD Operations ---

	// FindByID retrieves an author by ID
	FindByID(ctx context.Context, id author.ID) (*author.Author, error)

	// FindByName finds an author by first and last name
	FindByName(ctx context.Context, firstName, lastName string) (*author.Author, error)

	// Save persists an author (insert or update)
	Save(ctx context.Context, a *author.Author) error

	// GetOrCreate finds an existing author or creates a new one
	GetOrCreate(ctx context.Context, firstName, lastName string) (*author.Author, error)

	// Delete removes an author by ID
	Delete(ctx context.Context, id author.ID) error

	// --- Query Operations ---

	// Find returns authors matching the query (filters + sort + pagination)
	Find(ctx context.Context, query *AuthorQuery) ([]*author.Author, error)

	// FindWithCounts returns authors with their book counts
	FindWithCounts(ctx context.Context, query *AuthorQuery) ([]AuthorWithCount, error)

	// Count returns the total count of authors matching filters
	Count(ctx context.Context, filters *AuthorFilters) (int64, error)

	// --- Prefix Navigation ---

	// GetPrefixes returns distinct name prefixes for hierarchical navigation
	// length specifies the prefix length (1, 2, or 3 characters)
	// parentPrefix filters to prefixes starting with this string
	GetPrefixes(ctx context.Context, length int, parentPrefix string) ([]string, error)
}
