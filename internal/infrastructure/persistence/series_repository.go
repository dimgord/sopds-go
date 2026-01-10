package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/sopds/sopds-go/internal/domain/repository"
	"github.com/sopds/sopds-go/internal/domain/series"
)

// SeriesRepository implements repository.SeriesRepository using GORM
type SeriesRepository struct {
	db *gorm.DB
}

// NewSeriesRepository creates a new SeriesRepository
func NewSeriesRepository(db *DB) *SeriesRepository {
	return &SeriesRepository{db: db.DB}
}

// --- CRUD Operations ---

// FindByID retrieves a series by ID
func (r *SeriesRepository) FindByID(ctx context.Context, id series.ID) (*series.Series, error) {
	var model SeriesModel
	err := r.db.WithContext(ctx).
		Where("ser_id = ?", int64(id)).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find series by id: %w", err)
	}

	return SeriesToDomain(&model), nil
}

// FindByName finds a series by its name
func (r *SeriesRepository) FindByName(ctx context.Context, name string) (*series.Series, error) {
	var model SeriesModel
	err := r.db.WithContext(ctx).
		Where("ser = ?", name).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find series by name: %w", err)
	}

	return SeriesToDomain(&model), nil
}

// Save persists a series (insert or update)
func (r *SeriesRepository) Save(ctx context.Context, s *series.Series) error {
	model := SeriesToModel(s)

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "ser"}},
			DoNothing: true,
		}).
		Create(model).Error

	if err != nil {
		return fmt.Errorf("save series: %w", err)
	}

	return nil
}

// GetOrCreate finds an existing series or creates a new one
func (r *SeriesRepository) GetOrCreate(ctx context.Context, name string) (*series.Series, error) {
	// Try to find existing first
	existing, err := r.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	// Create new series
	model := &SeriesModel{
		Name: name,
	}

	err = r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "ser"}},
			DoNothing: true,
		}).
		Create(model).Error

	if err != nil {
		return nil, fmt.Errorf("create series: %w", err)
	}

	// If ID is 0, the record already existed and wasn't inserted
	if model.ID == 0 {
		return r.FindByName(ctx, name)
	}

	return SeriesToDomain(model), nil
}

// Delete removes a series by ID
func (r *SeriesRepository) Delete(ctx context.Context, id series.ID) error {
	err := r.db.WithContext(ctx).
		Delete(&SeriesModel{}, "ser_id = ?", int64(id)).Error
	if err != nil {
		return fmt.Errorf("delete series: %w", err)
	}
	return nil
}

// --- Query Operations ---

// Find returns series matching the query
func (r *SeriesRepository) Find(ctx context.Context, query *repository.SeriesQuery) ([]*series.Series, error) {
	var models []SeriesModel

	db := r.db.WithContext(ctx).Model(&SeriesModel{})

	if query != nil {
		db = db.Scopes(ApplySeriesFilters(query.Filters))
		db = db.Scopes(ApplyNameSort(query.Sort, "ser"))
		db = db.Scopes(Paginate(query.Pagination))
	} else {
		db = db.Scopes(SeriesWithBooks())
		db = db.Order("ser ASC")
	}

	if err := db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("find series: %w", err)
	}

	return SeriesModelsToDomain(models), nil
}

// FindWithCounts returns series with their book counts
func (r *SeriesRepository) FindWithCounts(ctx context.Context, query *repository.SeriesQuery) ([]repository.SeriesWithCount, error) {
	var results []struct {
		SeriesModel
		BookCount int64 `gorm:"column:book_count"`
	}

	db := r.db.WithContext(ctx).
		Table("series").
		Select("series.*, COUNT(DISTINCT bs.book_id) as book_count").
		Joins("LEFT JOIN bseries bs ON bs.ser_id = series.ser_id").
		Joins("LEFT JOIN books b ON bs.book_id = b.book_id AND b.avail != 0 AND b.duplicate_of IS NULL").
		Group("series.ser_id")

	if query != nil {
		db = db.Scopes(ApplySeriesFilters(query.Filters))

		// Special handling for sorting
		if query.Sort.Field == repository.SortByBookCount {
			dir := "ASC"
			if query.Sort.IsDescending() {
				dir = "DESC"
			}
			db = db.Order("book_count " + dir)
		} else {
			db = db.Scopes(ApplyNameSort(query.Sort, "series.ser"))
		}

		db = db.Scopes(Paginate(query.Pagination))
	} else {
		db = db.Having("COUNT(DISTINCT bs.book_id) > 0")
		db = db.Order("series.ser ASC")
	}

	if err := db.Find(&results).Error; err != nil {
		return nil, fmt.Errorf("find series with counts: %w", err)
	}

	seriesCounts := make([]repository.SeriesWithCount, len(results))
	for i := range results {
		seriesCounts[i] = repository.SeriesWithCount{
			Series:    SeriesToDomain(&results[i].SeriesModel),
			BookCount: results[i].BookCount,
		}
	}

	return seriesCounts, nil
}

// Count returns the total count of series matching filters
func (r *SeriesRepository) Count(ctx context.Context, filters *repository.SeriesFilters) (int64, error) {
	var count int64

	db := r.db.WithContext(ctx).Model(&SeriesModel{})
	db = db.Scopes(ApplySeriesFilters(filters))

	if err := db.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count series: %w", err)
	}

	return count, nil
}

// --- Prefix Navigation ---

// GetPrefixes returns distinct name prefixes for hierarchical navigation
func (r *SeriesRepository) GetPrefixes(ctx context.Context, length int, parentPrefix string) ([]string, error) {
	if length < 1 {
		length = 1
	}
	if length > 3 {
		length = 3
	}

	var prefixes []string

	// Build the query for prefix extraction
	query := r.db.WithContext(ctx).
		Table("series").
		Select(fmt.Sprintf("DISTINCT UPPER(LEFT(ser, %d)) as prefix", length)).
		Joins("JOIN bseries bs ON bs.ser_id = series.ser_id").
		Joins("JOIN books b ON bs.book_id = b.book_id AND b.avail != 0 AND b.duplicate_of IS NULL").
		Where("ser IS NOT NULL AND ser != ''").
		Order("prefix")

	if parentPrefix != "" {
		query = query.Where("UPPER(ser) LIKE ?", strings.ToUpper(parentPrefix)+"%")
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

// Ensure SeriesRepository implements the interface
var _ repository.SeriesRepository = (*SeriesRepository)(nil)
