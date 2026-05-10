package persistence

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dimgord/sopds-go/internal/domain/genre"
	"github.com/dimgord/sopds-go/internal/domain/repository"
)

// GenreRepository implements repository.GenreRepository using GORM
type GenreRepository struct {
	db *gorm.DB
}

// NewGenreRepository creates a new GenreRepository
func NewGenreRepository(db *DB) *GenreRepository {
	return &GenreRepository{db: db.DB}
}

// --- CRUD Operations ---

// FindByID retrieves a genre by ID
func (r *GenreRepository) FindByID(ctx context.Context, id genre.ID) (*genre.Genre, error) {
	var model GenreModel
	err := r.db.WithContext(ctx).
		Where("genre_id = ?", int64(id)).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find genre by id: %w", err)
	}

	return GenreToDomain(&model), nil
}

// FindByCode finds a genre by its code
func (r *GenreRepository) FindByCode(ctx context.Context, code string) (*genre.Genre, error) {
	var model GenreModel
	err := r.db.WithContext(ctx).
		Where("genre = ?", code).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find genre by code: %w", err)
	}

	return GenreToDomain(&model), nil
}

// Save persists a genre (insert or update)
func (r *GenreRepository) Save(ctx context.Context, g *genre.Genre) error {
	model := GenreToModel(g)

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "genre"}},
			UpdateAll: true,
		}).
		Create(model).Error

	if err != nil {
		return fmt.Errorf("save genre: %w", err)
	}

	return nil
}

// Delete removes a genre by ID
func (r *GenreRepository) Delete(ctx context.Context, id genre.ID) error {
	err := r.db.WithContext(ctx).
		Delete(&GenreModel{}, "genre_id = ?", int64(id)).Error
	if err != nil {
		return fmt.Errorf("delete genre: %w", err)
	}
	return nil
}

// --- Query Operations ---

// Find returns genres matching the query
func (r *GenreRepository) Find(ctx context.Context, query *repository.GenreQuery) ([]*genre.Genre, error) {
	var models []GenreModel

	db := r.db.WithContext(ctx).Model(&GenreModel{})

	if query != nil {
		db = db.Scopes(ApplyGenreFilters(query.Filters))
		db = db.Scopes(ApplyNameSort(query.Sort, "subsection"))
		db = db.Scopes(Paginate(query.Pagination))
	} else {
		db = db.Scopes(GenresWithBooks())
		db = db.Order("section ASC, subsection ASC")
	}

	if err := db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("find genres: %w", err)
	}

	return GenresModelsToDomain(models), nil
}

// FindWithCounts returns genres with their book counts
func (r *GenreRepository) FindWithCounts(ctx context.Context, query *repository.GenreQuery) ([]repository.GenreWithCount, error) {
	var results []struct {
		GenreModel
		BookCount int64 `gorm:"column:book_count"`
	}

	db := r.db.WithContext(ctx).
		Table("genres").
		Select("genres.*, COUNT(DISTINCT bg.book_id) as book_count").
		Joins("LEFT JOIN bgenres bg ON bg.genre_id = genres.genre_id").
		Joins("LEFT JOIN books b ON bg.book_id = b.book_id AND b.avail != 0 AND b.duplicate_of IS NULL").
		Group("genres.genre_id")

	if query != nil {
		db = db.Scopes(ApplyGenreFilters(query.Filters))

		// Special handling for sorting
		if query.Sort.Field == repository.SortByBookCount {
			dir := "ASC"
			if query.Sort.IsDescending() {
				dir = "DESC"
			}
			db = db.Order("book_count " + dir)
		} else {
			db = db.Order("genres.section ASC, genres.subsection ASC")
		}

		db = db.Scopes(Paginate(query.Pagination))
	} else {
		db = db.Having("COUNT(DISTINCT bg.book_id) > 0")
		db = db.Order("genres.section ASC, genres.subsection ASC")
	}

	if err := db.Find(&results).Error; err != nil {
		return nil, fmt.Errorf("find genres with counts: %w", err)
	}

	genreCounts := make([]repository.GenreWithCount, len(results))
	for i := range results {
		genreCounts[i] = repository.GenreWithCount{
			Genre:     GenreToDomain(&results[i].GenreModel),
			BookCount: results[i].BookCount,
		}
	}

	return genreCounts, nil
}

// Count returns the total count of genres matching filters
func (r *GenreRepository) Count(ctx context.Context, filters *repository.GenreFilters) (int64, error) {
	var count int64

	db := r.db.WithContext(ctx).Model(&GenreModel{})
	db = db.Scopes(ApplyGenreFilters(filters))

	if err := db.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count genres: %w", err)
	}

	return count, nil
}

// --- Section Navigation ---

// GetSections returns all distinct genre sections
func (r *GenreRepository) GetSections(ctx context.Context) ([]string, error) {
	var sections []string

	err := r.db.WithContext(ctx).
		Table("genres").
		Select("DISTINCT section").
		Joins("JOIN bgenres bg ON bg.genre_id = genres.genre_id").
		Joins("JOIN books b ON bg.book_id = b.book_id AND b.avail != 0 AND b.duplicate_of IS NULL").
		Where("section IS NOT NULL AND section != ''").
		Order("section ASC").
		Pluck("section", &sections).Error

	if err != nil {
		return nil, fmt.Errorf("get sections: %w", err)
	}

	return sections, nil
}

// FindBySection returns all genres in a section
func (r *GenreRepository) FindBySection(ctx context.Context, section string, sort repository.Sort) ([]*genre.Genre, error) {
	var models []GenreModel

	db := r.db.WithContext(ctx).
		Model(&GenreModel{}).
		Scopes(GenresWithBooks()).
		Where("section = ?", section)

	db = db.Scopes(ApplyNameSort(sort, "subsection"))

	if err := db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("find by section: %w", err)
	}

	return GenresModelsToDomain(models), nil
}

// Ensure GenreRepository implements the interface
var _ repository.GenreRepository = (*GenreRepository)(nil)
