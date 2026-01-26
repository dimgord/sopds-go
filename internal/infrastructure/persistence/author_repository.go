package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/sopds/sopds-go/internal/domain/author"
	"github.com/sopds/sopds-go/internal/domain/repository"
)

// AuthorRepository implements repository.AuthorRepository using GORM
type AuthorRepository struct {
	db *gorm.DB
}

// NewAuthorRepository creates a new AuthorRepository
func NewAuthorRepository(db *DB) *AuthorRepository {
	return &AuthorRepository{db: db.DB}
}

// --- CRUD Operations ---

// FindByID retrieves an author by ID
func (r *AuthorRepository) FindByID(ctx context.Context, id author.ID) (*author.Author, error) {
	var model AuthorModel
	err := r.db.WithContext(ctx).
		Where("author_id = ?", int64(id)).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find author by id: %w", err)
	}

	return AuthorToDomain(&model), nil
}

// FindByName finds an author by first and last name
func (r *AuthorRepository) FindByName(ctx context.Context, firstName, lastName string) (*author.Author, error) {
	var model AuthorModel
	err := r.db.WithContext(ctx).
		Where("first_name = ? AND last_name = ?", firstName, lastName).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find author by name: %w", err)
	}

	return AuthorToDomain(&model), nil
}

// Save persists an author (insert or update)
func (r *AuthorRepository) Save(ctx context.Context, a *author.Author) error {
	model := AuthorToModel(a)

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "first_name"}, {Name: "last_name"}},
			DoNothing: true,
		}).
		Create(model).Error

	if err != nil {
		return fmt.Errorf("save author: %w", err)
	}

	return nil
}

// GetOrCreate finds an existing author or creates a new one
// Uses ON CONFLICT to handle race conditions with concurrent workers
func (r *AuthorRepository) GetOrCreate(ctx context.Context, firstName, lastName string) (*author.Author, error) {
	model := &AuthorModel{
		FirstName: firstName,
		LastName:  lastName,
	}

	// Try to insert with ON CONFLICT DO NOTHING
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "first_name"}, {Name: "last_name"}},
			DoNothing: true,
		}).
		Create(model)

	if result.Error != nil {
		return nil, fmt.Errorf("create author: %w", result.Error)
	}

	// If no rows affected, the author already exists - fetch it
	if result.RowsAffected == 0 {
		existing, err := r.FindByName(ctx, firstName, lastName)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
		// Should not happen, but fallback
		return nil, fmt.Errorf("author not found after conflict: %s %s", firstName, lastName)
	}

	return AuthorToDomain(model), nil
}

// Delete removes an author by ID
func (r *AuthorRepository) Delete(ctx context.Context, id author.ID) error {
	err := r.db.WithContext(ctx).
		Delete(&AuthorModel{}, "author_id = ?", int64(id)).Error
	if err != nil {
		return fmt.Errorf("delete author: %w", err)
	}
	return nil
}

// --- Query Operations ---

// Find returns authors matching the query
func (r *AuthorRepository) Find(ctx context.Context, query *repository.AuthorQuery) ([]*author.Author, error) {
	var models []AuthorModel

	db := r.db.WithContext(ctx).Model(&AuthorModel{})

	if query != nil {
		db = db.Scopes(ApplyAuthorFilters(query.Filters))
		db = db.Scopes(ApplyNameSort(query.Sort, "last_name"))
		db = db.Scopes(Paginate(query.Pagination))
	} else {
		db = db.Scopes(AuthorsWithBooks())
		db = db.Order("last_name ASC, first_name ASC")
	}

	if err := db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("find authors: %w", err)
	}

	return AuthorsModelsToDomain(models), nil
}

// FindWithCounts returns authors with their book counts
func (r *AuthorRepository) FindWithCounts(ctx context.Context, query *repository.AuthorQuery) ([]repository.AuthorWithCount, error) {
	var results []struct {
		AuthorModel
		BookCount int64 `gorm:"column:book_count"`
	}

	db := r.db.WithContext(ctx).
		Table("authors").
		Select("authors.*, COUNT(DISTINCT ba.book_id) as book_count").
		Joins("LEFT JOIN bauthors ba ON ba.author_id = authors.author_id").
		Joins("LEFT JOIN books b ON ba.book_id = b.book_id AND b.avail != 0 AND b.duplicate_of IS NULL").
		Group("authors.author_id")

	if query != nil {
		db = db.Scopes(ApplyAuthorFilters(query.Filters))

		// Special handling for sorting
		if query.Sort.Field == repository.SortByBookCount {
			dir := "ASC"
			if query.Sort.IsDescending() {
				dir = "DESC"
			}
			db = db.Order("book_count " + dir)
		} else {
			db = db.Scopes(ApplyNameSort(query.Sort, "authors.last_name"))
		}

		db = db.Scopes(Paginate(query.Pagination))
	} else {
		db = db.Having("COUNT(DISTINCT ba.book_id) > 0")
		db = db.Order("authors.last_name ASC, authors.first_name ASC")
	}

	if err := db.Find(&results).Error; err != nil {
		return nil, fmt.Errorf("find authors with counts: %w", err)
	}

	authorCounts := make([]repository.AuthorWithCount, len(results))
	for i := range results {
		authorCounts[i] = repository.AuthorWithCount{
			Author:    AuthorToDomain(&results[i].AuthorModel),
			BookCount: results[i].BookCount,
		}
	}

	return authorCounts, nil
}

// Count returns the total count of authors matching filters
func (r *AuthorRepository) Count(ctx context.Context, filters *repository.AuthorFilters) (int64, error) {
	var count int64

	db := r.db.WithContext(ctx).Model(&AuthorModel{})
	db = db.Scopes(ApplyAuthorFilters(filters))

	if err := db.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count authors: %w", err)
	}

	return count, nil
}

// --- Prefix Navigation ---

// GetPrefixes returns distinct name prefixes for hierarchical navigation
func (r *AuthorRepository) GetPrefixes(ctx context.Context, length int, parentPrefix string) ([]string, error) {
	if length < 1 {
		length = 1
	}
	if length > 3 {
		length = 3
	}

	var prefixes []string

	// Build the query for prefix extraction
	// Use UPPER for case-insensitive grouping
	query := r.db.WithContext(ctx).
		Table("authors").
		Select(fmt.Sprintf("DISTINCT UPPER(LEFT(last_name, %d)) as prefix", length)).
		Joins("JOIN bauthors ba ON ba.author_id = authors.author_id").
		Joins("JOIN books b ON ba.book_id = b.book_id AND b.avail != 0 AND b.duplicate_of IS NULL").
		Where("last_name IS NOT NULL AND last_name != ''").
		Order("prefix")

	if parentPrefix != "" {
		query = query.Where("UPPER(last_name) LIKE ?", strings.ToUpper(parentPrefix)+"%")
	}

	rows, err := query.Rows()
	if err != nil {
		return nil, fmt.Errorf("get prefixes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var prefix string
		if err := rows.Scan(&prefix); err != nil {
			return nil, fmt.Errorf("scan prefix: %w", err)
		}
		if prefix != "" {
			prefixes = append(prefixes, prefix)
		}
	}

	return prefixes, nil
}

// Ensure AuthorRepository implements the interface
var _ repository.AuthorRepository = (*AuthorRepository)(nil)
