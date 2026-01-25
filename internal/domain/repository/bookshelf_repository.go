package repository

import (
	"context"

	"github.com/sopds/sopds-go/internal/domain/book"
)

// BookshelfRepository defines the interface for user bookshelf persistence
type BookshelfRepository interface {
	// --- CRUD Operations ---

	// Add adds a book to user's bookshelf
	Add(ctx context.Context, username string, bookID book.ID) error

	// Remove removes a book from user's bookshelf
	Remove(ctx context.Context, username string, bookID book.ID) error

	// Exists checks if a book is in user's bookshelf
	Exists(ctx context.Context, username string, bookID book.ID) (bool, error)

	// --- Query Operations ---

	// GetBooks returns books from user's bookshelf
	// Supports filtering, sorting, and pagination
	GetBooks(ctx context.Context, username string, filters *BookFilters, sort Sort, pagination *Pagination) ([]*book.Book, error)

	// Count returns the number of books in user's bookshelf
	Count(ctx context.Context, username string) (int64, error)

	// GetBookIDs returns all book IDs on user's bookshelf as a set for efficient lookup
	GetBookIDs(ctx context.Context, username string) (map[int64]bool, error)

	// --- Bulk Operations ---

	// Clear removes all books from user's bookshelf
	Clear(ctx context.Context, username string) error
}
