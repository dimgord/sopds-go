package persistence

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/sopds/sopds-go/internal/domain/book"
	"github.com/sopds/sopds-go/internal/domain/repository"
)

// BookshelfRepository implements repository.BookshelfRepository using GORM
type BookshelfRepository struct {
	db *gorm.DB
}

// NewBookshelfRepository creates a new BookshelfRepository
func NewBookshelfRepository(db *DB) *BookshelfRepository {
	return &BookshelfRepository{db: db.DB}
}

// --- CRUD Operations ---

// Add adds a book to user's bookshelf
func (r *BookshelfRepository) Add(ctx context.Context, username string, bookID book.ID) error {
	model := BookshelfModel{
		UserName: username,
		BookID:   int64(bookID),
	}

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model).Error

	if err != nil {
		return fmt.Errorf("add to bookshelf: %w", err)
	}

	return nil
}

// Remove removes a book from user's bookshelf
func (r *BookshelfRepository) Remove(ctx context.Context, username string, bookID book.ID) error {
	err := r.db.WithContext(ctx).
		Where("user_name = ? AND book_id = ?", username, int64(bookID)).
		Delete(&BookshelfModel{}).Error

	if err != nil {
		return fmt.Errorf("remove from bookshelf: %w", err)
	}

	return nil
}

// Exists checks if a book is in user's bookshelf
func (r *BookshelfRepository) Exists(ctx context.Context, username string, bookID book.ID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&BookshelfModel{}).
		Where("user_name = ? AND book_id = ?", username, int64(bookID)).
		Count(&count).Error

	if err != nil {
		return false, fmt.Errorf("check bookshelf: %w", err)
	}

	return count > 0, nil
}

// --- Query Operations ---

// GetBooks returns books from user's bookshelf
func (r *BookshelfRepository) GetBooks(ctx context.Context, username string, filters *repository.BookFilters, sort repository.Sort, pagination *repository.Pagination) ([]*book.Book, error) {
	var models []BookModel

	db := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Joins("JOIN bookshelf bs ON bs.book_id = books.book_id").
		Where("bs.user_name = ?", username)

	// Apply book filters
	db = db.Scopes(ApplyBookFilters(filters))
	db = db.Scopes(ApplyBookSort(sort))
	db = db.Scopes(Paginate(pagination))

	if err := db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("get bookshelf books: %w", err)
	}

	return BooksModelsToDomain(models), nil
}

// Count returns the number of books in user's bookshelf
func (r *BookshelfRepository) Count(ctx context.Context, username string) (int64, error) {
	var count int64

	err := r.db.WithContext(ctx).
		Model(&BookshelfModel{}).
		Where("user_name = ?", username).
		Count(&count).Error

	if err != nil {
		return 0, fmt.Errorf("count bookshelf: %w", err)
	}

	return count, nil
}

// --- Bulk Operations ---

// Clear removes all books from user's bookshelf
func (r *BookshelfRepository) Clear(ctx context.Context, username string) error {
	err := r.db.WithContext(ctx).
		Where("user_name = ?", username).
		Delete(&BookshelfModel{}).Error

	if err != nil {
		return fmt.Errorf("clear bookshelf: %w", err)
	}

	return nil
}

// Ensure BookshelfRepository implements the interface
var _ repository.BookshelfRepository = (*BookshelfRepository)(nil)
