package persistence

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dimgord/sopds-go/internal/domain/book"
	"github.com/dimgord/sopds-go/internal/domain/repository"
)

// BookRepository implements repository.BookRepository using GORM
type BookRepository struct {
	db *gorm.DB
}

// NewBookRepository creates a new BookRepository
func NewBookRepository(db *DB) *BookRepository {
	return &BookRepository{db: db.DB}
}

// --- CRUD Operations ---

// FindByID retrieves a book by its ID
func (r *BookRepository) FindByID(ctx context.Context, id book.ID) (*book.Book, error) {
	var model BookModel
	err := r.db.WithContext(ctx).
		Where("book_id = ?", int64(id)).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find book by id: %w", err)
	}

	return BookToDomain(&model), nil
}

// FindByFilenameAndPath finds a book by filename and path
func (r *BookRepository) FindByFilenameAndPath(ctx context.Context, filename, path string) (*book.Book, error) {
	var model BookModel
	err := r.db.WithContext(ctx).
		Where("filename = ? AND path = ?", filename, path).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find book by filename and path: %w", err)
	}

	return BookToDomain(&model), nil
}

// GetBookMapByCatalog returns a map of "path/filename" -> book_id for all books in a catalog.
// Used for efficient existence checking during ZIP scans.
func (r *BookRepository) GetBookMapByCatalog(ctx context.Context, catalogID int64) (map[string]int64, error) {
	var results []struct {
		BookID   int64  `gorm:"column:book_id"`
		Filename string `gorm:"column:filename"`
		Path     string `gorm:"column:path"`
	}

	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Select("book_id, filename, path").
		Where("cat_id = ?", catalogID).
		Find(&results).Error

	if err != nil {
		return nil, fmt.Errorf("get book map by catalog: %w", err)
	}

	bookMap := make(map[string]int64, len(results))
	for _, r := range results {
		// Key is just the path (which already includes filename for ZIP books)
		bookMap[r.Path] = r.BookID
	}

	return bookMap, nil
}

// Save persists a book (insert or update)
func (r *BookRepository) Save(ctx context.Context, b *book.Book) error {
	model := BookToModel(b)

	// Use Clauses to handle upsert
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "filename"}, {Name: "path"}},
			UpdateAll: true,
		}).
		Create(model).Error

	if err != nil {
		return fmt.Errorf("save book: %w", err)
	}

	// Update domain book with the ID from database (populated by RETURNING)
	b.SetID(book.ID(model.ID))

	return nil
}

// SaveBatch persists multiple books in a single transaction
func (r *BookRepository) SaveBatch(ctx context.Context, books []*book.Book) ([]book.ID, error) {
	if len(books) == 0 {
		return nil, nil
	}

	models := make([]*BookModel, len(books))
	for i, b := range books {
		models[i] = BookToModel(b)
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "filename"}, {Name: "path"}},
			UpdateAll: true,
		}).CreateInBatches(models, 100).Error
	})

	if err != nil {
		return nil, fmt.Errorf("save batch: %w", err)
	}

	ids := make([]book.ID, len(models))
	for i, m := range models {
		ids[i] = book.ID(m.ID)
	}

	return ids, nil
}

// Delete removes a book by ID
func (r *BookRepository) Delete(ctx context.Context, id book.ID) error {
	err := r.db.WithContext(ctx).
		Delete(&BookModel{}, "book_id = ?", int64(id)).Error
	if err != nil {
		return fmt.Errorf("delete book: %w", err)
	}
	return nil
}

// --- Query Operations ---

// Find returns books matching the query
func (r *BookRepository) Find(ctx context.Context, query *repository.BookQuery) ([]*book.Book, error) {
	var models []BookModel

	db := r.db.WithContext(ctx).Model(&BookModel{})

	// Apply filters
	if query != nil {
		db = db.Scopes(ApplyBookFilters(query.Filters))
		db = db.Scopes(ApplyBookSort(query.Sort))
		db = db.Scopes(Paginate(query.Pagination))
	} else {
		db = db.Scopes(AvailableBooks(), ExcludeDuplicates())
	}

	if err := db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("find books: %w", err)
	}

	return BooksModelsToDomain(models), nil
}

// Count returns the total count of books matching filters
func (r *BookRepository) Count(ctx context.Context, filters *repository.BookFilters) (int64, error) {
	var count int64

	db := r.db.WithContext(ctx).Model(&BookModel{})
	db = db.Scopes(ApplyBookFilters(filters))

	if err := db.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count books: %w", err)
	}

	return count, nil
}

// --- Relationship Loading ---

// LoadAuthors loads authors for a book
func (r *BookRepository) LoadAuthors(ctx context.Context, bookID book.ID) ([]book.AuthorRef, error) {
	var authors []AuthorModel

	err := r.db.WithContext(ctx).
		Table("authors").
		Joins("JOIN bauthors ON bauthors.author_id = authors.author_id").
		Where("bauthors.book_id = ?", int64(bookID)).
		Order("authors.last_name, authors.first_name").
		Find(&authors).Error

	if err != nil {
		return nil, fmt.Errorf("load authors: %w", err)
	}

	return AuthorsToRefs(authors), nil
}

// LoadGenres loads genres for a book
func (r *BookRepository) LoadGenres(ctx context.Context, bookID book.ID) ([]book.GenreRef, error) {
	var genres []GenreModel

	err := r.db.WithContext(ctx).
		Table("genres").
		Joins("JOIN bgenres ON bgenres.genre_id = genres.genre_id").
		Where("bgenres.book_id = ?", int64(bookID)).
		Order("genres.section, genres.subsection").
		Find(&genres).Error

	if err != nil {
		return nil, fmt.Errorf("load genres: %w", err)
	}

	return GenresToRefs(genres), nil
}

// LoadSeries loads series for a book
func (r *BookRepository) LoadSeries(ctx context.Context, bookID book.ID) ([]book.SeriesRef, error) {
	var results []struct {
		SeriesModel
		SerNo int `gorm:"column:ser_no"`
	}

	err := r.db.WithContext(ctx).
		Table("series").
		Select("series.*, bseries.ser_no").
		Joins("JOIN bseries ON bseries.ser_id = series.ser_id").
		Where("bseries.book_id = ?", int64(bookID)).
		Order("series.ser").
		Find(&results).Error

	if err != nil {
		return nil, fmt.Errorf("load series: %w", err)
	}

	refs := make([]book.SeriesRef, len(results))
	for i, r := range results {
		refs[i] = book.SeriesRef{
			ID:     r.ID,
			Name:   r.Name,
			Number: r.SerNo,
		}
	}

	return refs, nil
}

// LoadRelationships loads all relationships for a book
func (r *BookRepository) LoadRelationships(ctx context.Context, bookID book.ID) ([]book.AuthorRef, []book.GenreRef, []book.SeriesRef, error) {
	authors, err := r.LoadAuthors(ctx, bookID)
	if err != nil {
		return nil, nil, nil, err
	}

	genres, err := r.LoadGenres(ctx, bookID)
	if err != nil {
		return nil, nil, nil, err
	}

	series, err := r.LoadSeries(ctx, bookID)
	if err != nil {
		return nil, nil, nil, err
	}

	return authors, genres, series, nil
}

// --- Relationship Management ---

// AddAuthor links a book to an author
func (r *BookRepository) AddAuthor(ctx context.Context, bookID book.ID, authorID int64) error {
	model := BookAuthorModel{
		BookID:   int64(bookID),
		AuthorID: authorID,
	}

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model).Error

	if err != nil {
		return fmt.Errorf("add author: %w", err)
	}
	return nil
}

// AddAuthorsBatch links multiple book-author pairs
func (r *BookRepository) AddAuthorsBatch(ctx context.Context, pairs [][2]int64) error {
	if len(pairs) == 0 {
		return nil
	}

	models := make([]BookAuthorModel, len(pairs))
	for i, pair := range pairs {
		models[i] = BookAuthorModel{
			BookID:   pair[0],
			AuthorID: pair[1],
		}
	}

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(models, 100).Error

	if err != nil {
		return fmt.Errorf("add authors batch: %w", err)
	}
	return nil
}

// AddAuthorWithRole links a book to an author with a specific role
func (r *BookRepository) AddAuthorWithRole(ctx context.Context, bookID book.ID, authorID int64, role string) error {
	model := BookAuthorModel{
		BookID:   int64(bookID),
		AuthorID: authorID,
		Role:     role,
	}

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model).Error

	if err != nil {
		return fmt.Errorf("add author with role: %w", err)
	}
	return nil
}

// LoadNarrators loads narrators (authors with role='narrator') for a book
func (r *BookRepository) LoadNarrators(ctx context.Context, bookID book.ID) ([]book.AuthorRef, error) {
	var authors []AuthorModel

	err := r.db.WithContext(ctx).
		Table("authors").
		Joins("JOIN bauthors ON bauthors.author_id = authors.author_id").
		Where("bauthors.book_id = ? AND bauthors.role = ?", int64(bookID), RoleNarrator).
		Order("authors.last_name, authors.first_name").
		Find(&authors).Error

	if err != nil {
		return nil, fmt.Errorf("load narrators: %w", err)
	}

	return AuthorsToRefs(authors), nil
}

// LoadAuthorsByRole loads authors with a specific role for a book
func (r *BookRepository) LoadAuthorsByRole(ctx context.Context, bookID book.ID, role string) ([]book.AuthorRef, error) {
	var authors []AuthorModel

	err := r.db.WithContext(ctx).
		Table("authors").
		Joins("JOIN bauthors ON bauthors.author_id = authors.author_id").
		Where("bauthors.book_id = ? AND bauthors.role = ?", int64(bookID), role).
		Order("authors.last_name, authors.first_name").
		Find(&authors).Error

	if err != nil {
		return nil, fmt.Errorf("load authors by role: %w", err)
	}

	return AuthorsToRefs(authors), nil
}

// AddGenre links a book to a genre
func (r *BookRepository) AddGenre(ctx context.Context, bookID book.ID, genreID int64) error {
	model := BookGenreModel{
		BookID:  int64(bookID),
		GenreID: genreID,
	}

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model).Error

	if err != nil {
		return fmt.Errorf("add genre: %w", err)
	}
	return nil
}

// AddSeries links a book to a series with order number
func (r *BookRepository) AddSeries(ctx context.Context, bookID book.ID, seriesID int64, number int) error {
	model := BookSeriesModel{
		BookID:   int64(bookID),
		SeriesID: seriesID,
		SerNo:    number,
	}

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model).Error

	if err != nil {
		return fmt.Errorf("add series: %w", err)
	}
	return nil
}

// --- Availability Management ---

// UpdateAvailability changes a book's availability status
func (r *BookRepository) UpdateAvailability(ctx context.Context, id book.ID, avail book.Availability) error {
	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Where("book_id = ?", int64(id)).
		Update("avail", int(avail)).Error

	if err != nil {
		return fmt.Errorf("update availability: %w", err)
	}
	return nil
}

// UpdateAvailabilityBatch updates availability for multiple books in a single query
func (r *BookRepository) UpdateAvailabilityBatch(ctx context.Context, ids []int64, avail book.Availability) error {
	if len(ids) == 0 {
		return nil
	}

	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Where("book_id IN ?", ids).
		Update("avail", int(avail)).Error

	if err != nil {
		return fmt.Errorf("update availability batch: %w", err)
	}
	return nil
}

// MarkAllPending marks all verified books as pending
func (r *BookRepository) MarkAllPending(ctx context.Context) error {
	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Where("avail = ?", int(book.AvailabilityVerified)).
		Update("avail", int(book.AvailabilityPending)).Error

	if err != nil {
		return fmt.Errorf("mark all pending: %w", err)
	}
	return nil
}

// MarkCatalogVerified marks all pending books in a catalog as verified
func (r *BookRepository) MarkCatalogVerified(ctx context.Context, catalogID int64) error {
	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Where("cat_id = ? AND avail = ?", catalogID, int(book.AvailabilityPending)).
		Update("avail", int(book.AvailabilityVerified)).Error

	if err != nil {
		return fmt.Errorf("mark catalog verified: %w", err)
	}
	return nil
}

// MarkCatalogsVerified marks all pending books in multiple catalogs as verified
func (r *BookRepository) MarkCatalogsVerified(ctx context.Context, catalogIDs []int64) error {
	if len(catalogIDs) == 0 {
		return nil
	}

	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Where("cat_id IN ? AND avail = ?", catalogIDs, int(book.AvailabilityPending)).
		Update("avail", int(book.AvailabilityVerified)).Error

	if err != nil {
		return fmt.Errorf("mark catalogs verified: %w", err)
	}
	return nil
}

// DeleteUnavailable deletes books that are still pending after scan
func (r *BookRepository) DeleteUnavailable(ctx context.Context, physical bool) (int64, error) {
	var result *gorm.DB

	if physical {
		// Actually delete from database
		result = r.db.WithContext(ctx).
			Where("avail = ?", int(book.AvailabilityPending)).
			Delete(&BookModel{})
	} else {
		// Mark as deleted
		result = r.db.WithContext(ctx).
			Model(&BookModel{}).
			Where("avail = ?", int(book.AvailabilityPending)).
			Update("avail", int(book.AvailabilityDeleted))
	}

	if result.Error != nil {
		return 0, fmt.Errorf("delete unavailable: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// DeleteInCatalogs deletes all books in the specified catalogs and the catalogs themselves
func (r *BookRepository) DeleteInCatalogs(ctx context.Context, catalogIDs []int64) (int64, error) {
	if len(catalogIDs) == 0 {
		return 0, nil
	}

	// Delete books in these catalogs
	result := r.db.WithContext(ctx).
		Where("cat_id IN ?", catalogIDs).
		Delete(&BookModel{})

	if result.Error != nil {
		return 0, fmt.Errorf("delete books in catalogs: %w", result.Error)
	}
	booksDeleted := result.RowsAffected

	// Also delete the catalog entries themselves
	catResult := r.db.WithContext(ctx).
		Where("cat_id IN ?", catalogIDs).
		Delete(&CatalogModel{})

	if catResult.Error != nil {
		return booksDeleted, fmt.Errorf("delete catalogs: %w", catResult.Error)
	}

	return booksDeleted, nil
}

// --- Duplicate Management ---

// MarkDuplicates runs duplicate detection and marks duplicates
func (r *BookRepository) MarkDuplicates(ctx context.Context, mode repository.DuplicateMode) error {
	// First, clear existing duplicate markers
	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Where("duplicate_of IS NOT NULL").
		Update("duplicate_of", nil).Error
	if err != nil {
		return fmt.Errorf("clear duplicates: %w", err)
	}

	if mode == repository.DuplicateModeNone || mode == repository.DuplicateModeClear {
		return nil
	}

	// Use raw SQL for complex window functions
	var sql string

	switch mode {
	case repository.DuplicateModeNormal:
		// Normal mode: group by title + authors
		sql = `
			WITH book_authors AS (
				SELECT b.book_id, b.title,
					STRING_AGG(COALESCE(a.last_name, ''), ',' ORDER BY a.author_id) as authors
				FROM books b
				LEFT JOIN bauthors ba ON b.book_id = ba.book_id
				LEFT JOIN authors a ON ba.author_id = a.author_id
				WHERE b.avail != 0
				GROUP BY b.book_id, b.title
			),
			duplicates AS (
				SELECT book_id,
					FIRST_VALUE(book_id) OVER (
						PARTITION BY LOWER(title), authors
						ORDER BY book_id
					) as original_id
				FROM book_authors
			)
			UPDATE books SET duplicate_of = d.original_id
			FROM duplicates d
			WHERE books.book_id = d.book_id
			AND books.book_id != d.original_id`

	case repository.DuplicateModeStrong:
		// Strong mode: group by title + format + filesize
		sql = `
			WITH duplicates AS (
				SELECT book_id,
					FIRST_VALUE(book_id) OVER (
						PARTITION BY LOWER(title), format, filesize
						ORDER BY book_id
					) as original_id
				FROM books
				WHERE avail != 0
			)
			UPDATE books SET duplicate_of = d.original_id
			FROM duplicates d
			WHERE books.book_id = d.book_id
			AND books.book_id != d.original_id`
	}

	if err := r.db.WithContext(ctx).Exec(sql).Error; err != nil {
		return fmt.Errorf("mark duplicates: %w", err)
	}

	return nil
}

// MarkDuplicatesIncremental marks duplicates only for newly added books
// This is O(n) for n new books instead of O(N log N) for N total books
func (r *BookRepository) MarkDuplicatesIncremental(ctx context.Context, mode repository.DuplicateMode, newBookIDs []int64, progressFn func(processed, total int)) error {
	if mode == repository.DuplicateModeNone || mode == repository.DuplicateModeClear || len(newBookIDs) == 0 {
		return nil
	}

	total := len(newBookIDs)
	batchSize := 100
	processed := 0

	// Process in batches for progress reporting
	for i := 0; i < total; i += batchSize {
		end := i + batchSize
		if end > total {
			end = total
		}
		batch := newBookIDs[i:end]

		var sql string
		switch mode {
		case repository.DuplicateModeNormal:
			// For each new book, find existing book with same LOWER(title) and authors
			// Only compute author strings for books with matching titles (much faster)
			// Audiobooks only match audiobooks, non-audiobooks only match non-audiobooks
			sql = `
				WITH new_books AS (
					SELECT book_id, LOWER(title) as ltitle, is_audiobook
					FROM books WHERE book_id = ANY($1)
				),
				new_book_authors AS (
					SELECT b.book_id, LOWER(b.title) as ltitle, b.is_audiobook,
						STRING_AGG(COALESCE(a.last_name, ''), ',' ORDER BY a.author_id) as authors
					FROM books b
					LEFT JOIN bauthors ba ON b.book_id = ba.book_id
					LEFT JOIN authors a ON ba.author_id = a.author_id
					WHERE b.book_id = ANY($1)
					GROUP BY b.book_id, b.title, b.is_audiobook
				),
				candidate_existing AS (
					SELECT DISTINCT b.book_id, LOWER(b.title) as ltitle, b.is_audiobook
					FROM books b
					JOIN new_books n ON LOWER(b.title) = n.ltitle AND b.is_audiobook = n.is_audiobook
					WHERE b.avail != 0 AND b.book_id != ALL($1)
				),
				existing_book_authors AS (
					SELECT b.book_id, LOWER(b.title) as ltitle, b.is_audiobook,
						STRING_AGG(COALESCE(a.last_name, ''), ',' ORDER BY a.author_id) as authors
					FROM books b
					LEFT JOIN bauthors ba ON b.book_id = ba.book_id
					LEFT JOIN authors a ON ba.author_id = a.author_id
					WHERE b.book_id IN (SELECT book_id FROM candidate_existing)
					GROUP BY b.book_id, b.title, b.is_audiobook
				)
				UPDATE books
				SET duplicate_of = e.book_id
				FROM new_book_authors n
				JOIN existing_book_authors e ON n.ltitle = e.ltitle AND n.authors = e.authors AND n.is_audiobook = e.is_audiobook
				WHERE books.book_id = n.book_id
				AND e.book_id < n.book_id`

		case repository.DuplicateModeStrong:
			// For each new book, find existing book with same title+format+filesize
			// Audiobooks only match audiobooks, non-audiobooks only match non-audiobooks
			sql = `
				UPDATE books new
				SET duplicate_of = (
					SELECT MIN(existing.book_id)
					FROM books existing
					WHERE existing.avail != 0
					AND existing.book_id < new.book_id
					AND LOWER(existing.title) = LOWER(new.title)
					AND existing.format = new.format
					AND existing.filesize = new.filesize
					AND existing.is_audiobook = new.is_audiobook
				)
				WHERE new.book_id = ANY($1)
				AND EXISTS (
					SELECT 1 FROM books existing
					WHERE existing.avail != 0
					AND existing.book_id < new.book_id
					AND LOWER(existing.title) = LOWER(new.title)
					AND existing.format = new.format
					AND existing.filesize = new.filesize
					AND existing.is_audiobook = new.is_audiobook
				)`
		}

		if err := r.db.WithContext(ctx).Exec(sql, batch).Error; err != nil {
			return fmt.Errorf("mark duplicates batch: %w", err)
		}

		processed = end
		if progressFn != nil {
			progressFn(processed, total)
		}
	}

	return nil
}

// FindDuplicates returns all books that are duplicates of the given book
func (r *BookRepository) FindDuplicates(ctx context.Context, bookID book.ID, pagination *repository.Pagination) ([]*book.Book, error) {
	var models []BookModel

	// First, find the original book ID (if this is a duplicate) or use the given ID
	var originalID int64
	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Select("COALESCE(duplicate_of, book_id)").
		Where("book_id = ?", int64(bookID)).
		Scan(&originalID).Error

	if err != nil {
		return nil, fmt.Errorf("find original: %w", err)
	}

	// Find all duplicates (including the original)
	db := r.db.WithContext(ctx).
		Where("book_id = ? OR duplicate_of = ?", originalID, originalID).
		Order("book_id")

	db = db.Scopes(Paginate(pagination))

	if err := db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("find duplicates: %w", err)
	}

	return BooksModelsToDomain(models), nil
}

// CountDuplicates returns the number of duplicates for a book
func (r *BookRepository) CountDuplicates(ctx context.Context, bookID book.ID) (int, error) {
	var count int64

	// Find the original ID first
	var originalID int64
	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Select("COALESCE(duplicate_of, book_id)").
		Where("book_id = ?", int64(bookID)).
		Scan(&originalID).Error

	if err != nil {
		return 0, fmt.Errorf("find original: %w", err)
	}

	// Count all related books
	err = r.db.WithContext(ctx).
		Model(&BookModel{}).
		Where("book_id = ? OR duplicate_of = ?", originalID, originalID).
		Count(&count).Error

	if err != nil {
		return 0, fmt.Errorf("count duplicates: %w", err)
	}

	return int(count), nil
}

// --- Statistics ---

// GetLanguageStats returns languages with book counts
func (r *BookRepository) GetLanguageStats(ctx context.Context) ([]repository.LanguageStats, error) {
	var results []struct {
		Code  string `gorm:"column:lang"`
		Count int64  `gorm:"column:cnt"`
	}

	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Select("lang, COUNT(*) as cnt").
		Scopes(AvailableBooks(), ExcludeDuplicates()).
		Group("lang").
		Order("cnt DESC").
		Find(&results).Error

	if err != nil {
		return nil, fmt.Errorf("get language stats: %w", err)
	}

	stats := make([]repository.LanguageStats, len(results))
	for i, r := range results {
		stats[i] = repository.LanguageStats{
			Code:  r.Code,
			Count: r.Count,
		}
	}

	return stats, nil
}

// GetFormatStats returns formats with book counts
func (r *BookRepository) GetFormatStats(ctx context.Context) ([]repository.FormatStats, error) {
	var results []struct {
		Format string `gorm:"column:format"`
		Count  int64  `gorm:"column:cnt"`
	}

	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Select("format, COUNT(*) as cnt").
		Scopes(AvailableBooks(), ExcludeDuplicates()).
		Group("format").
		Order("cnt DESC").
		Find(&results).Error

	if err != nil {
		return nil, fmt.Errorf("get format stats: %w", err)
	}

	stats := make([]repository.FormatStats, len(results))
	for i, r := range results {
		stats[i] = repository.FormatStats{
			Format: r.Format,
			Count:  r.Count,
		}
	}

	return stats, nil
}

// GetDistinctLanguages returns all distinct language codes
func (r *BookRepository) GetDistinctLanguages(ctx context.Context) ([]string, error) {
	var languages []string

	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Distinct("lang").
		Scopes(AvailableBooks(), ExcludeDuplicates()).
		Where("lang != ''").
		Order("lang").
		Pluck("lang", &languages).Error

	if err != nil {
		return nil, fmt.Errorf("get distinct languages: %w", err)
	}

	return languages, nil
}

// GetDistinctFormats returns all distinct format codes
func (r *BookRepository) GetDistinctFormats(ctx context.Context) ([]string, error) {
	var formats []string

	err := r.db.WithContext(ctx).
		Model(&BookModel{}).
		Distinct("format").
		Scopes(AvailableBooks(), ExcludeDuplicates()).
		Where("format != ''").
		Order("format").
		Pluck("format", &formats).Error

	if err != nil {
		return nil, fmt.Errorf("get distinct formats: %w", err)
	}

	return formats, nil
}

// Ensure BookRepository implements the interface
var _ repository.BookRepository = (*BookRepository)(nil)
