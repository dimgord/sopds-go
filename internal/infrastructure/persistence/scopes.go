package persistence

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/sopds/sopds-go/internal/domain/book"
	"github.com/sopds/sopds-go/internal/domain/repository"
)

// --- Book Scopes ---

// AvailableBooks filters to only available (non-deleted) books
func AvailableBooks() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("avail != 0")
	}
}

// ExcludeDuplicates filters out duplicate books
func ExcludeDuplicates() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("duplicate_of IS NULL")
	}
}

// WithAvailability applies availability filter based on BookFilters
func WithAvailability(filters *repository.BookFilters) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if filters == nil || !filters.ShowDeleted {
			db = db.Where("books.avail != 0")
		}
		if filters == nil || !filters.ShowDuplicates {
			db = db.Where("books.duplicate_of IS NULL")
		}
		return db
	}
}

// WithLanguages filters by language codes
func WithLanguages(langs []book.Language) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(langs) == 0 {
			return db
		}
		langStrs := make([]string, len(langs))
		for i, l := range langs {
			langStrs[i] = l.String()
		}
		return db.Where("books.lang IN ?", langStrs)
	}
}

// WithFormats filters by format codes
func WithFormats(formats []book.Format) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(formats) == 0 {
			return db
		}
		formatStrs := make([]string, len(formats))
		for i, f := range formats {
			formatStrs[i] = f.String()
		}
		return db.Where("books.format IN ?", formatStrs)
	}
}

// WithSizeRange filters by filesize range
func WithSizeRange(min, max *int64) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if min != nil {
			db = db.Where("books.filesize >= ?", *min)
		}
		if max != nil {
			db = db.Where("books.filesize <= ?", *max)
		}
		return db
	}
}

// WithinPeriod filters books registered within the given number of days
func WithinPeriod(days int) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if days <= 0 {
			return db
		}
		cutoff := time.Now().AddDate(0, 0, -days)
		return db.Where("books.registerdate >= ?", cutoff)
	}
}

// WithTitlePrefix filters by title prefix (case-insensitive)
func WithTitlePrefix(prefix string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if prefix == "" {
			return db
		}
		return db.Where("LOWER(books.title) LIKE ?", strings.ToLower(prefix)+"%")
	}
}

// WithKeywords searches in title (and optionally annotation) using full-text search
func WithKeywords(keywords []string, includeAnnotation bool) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(keywords) == 0 {
			return db
		}

		if includeAnnotation {
			// Use FTS on combined search_vector (title + annotation)
			queryParts := make([]string, len(keywords))
			for i, kw := range keywords {
				escaped := strings.ReplaceAll(kw, "'", "''")
				queryParts[i] = escaped + ":*"
			}
			tsQuery := strings.Join(queryParts, " & ")
			return db.Where("books.search_vector @@ to_tsquery('simple', ?)", tsQuery)
		}

		// Title-only search using ILIKE (simpler, no annotation)
		for _, kw := range keywords {
			pattern := "%" + strings.ToLower(kw) + "%"
			db = db.Where("LOWER(books.title) LIKE ?", pattern)
		}
		return db
	}
}

// WithAuthors filters by author IDs (book has ANY of the authors)
func WithAuthors(authorIDs []int64) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(authorIDs) == 0 {
			return db
		}
		return db.Where("EXISTS (SELECT 1 FROM bauthors ba WHERE ba.book_id = books.book_id AND ba.author_id IN ?)", authorIDs)
	}
}

// WithGenres filters by genre IDs (book has ANY of the genres)
func WithGenres(genreIDs []int64) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(genreIDs) == 0 {
			return db
		}
		return db.Where("EXISTS (SELECT 1 FROM bgenres bg WHERE bg.book_id = books.book_id AND bg.genre_id IN ?)", genreIDs)
	}
}

// WithSeries filters by series IDs (book is in ANY of the series)
func WithSeries(seriesIDs []int64) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(seriesIDs) == 0 {
			return db
		}
		return db.Where("EXISTS (SELECT 1 FROM bseries bs WHERE bs.book_id = books.book_id AND bs.ser_id IN ?)", seriesIDs)
	}
}

// WithCatalogs filters by catalog IDs
func WithCatalogs(catalogIDs []int64) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(catalogIDs) == 0 {
			return db
		}
		return db.Where("books.cat_id IN ?", catalogIDs)
	}
}

// OnlyFavorites filters to only favorite books
func OnlyFavorites() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("books.favorite != 0")
	}
}

// OnlyAudiobooks filters to only audiobook entries
func OnlyAudiobooks() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("books.is_audiobook = true")
	}
}

// WithAuthorName searches in author first+last name (ILIKE)
func WithAuthorName(query string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if query == "" {
			return db
		}
		pattern := "%" + strings.ToLower(query) + "%"
		return db.Where(`EXISTS (
			SELECT 1 FROM bauthors ba
			JOIN authors a ON ba.author_id = a.author_id
			WHERE ba.book_id = books.book_id
			AND (LOWER(a.first_name) LIKE ? OR LOWER(a.last_name) LIKE ?
				OR LOWER(COALESCE(a.last_name, '') || ' ' || COALESCE(a.first_name, '')) LIKE ?
				OR LOWER(COALESCE(a.first_name, '') || ' ' || COALESCE(a.last_name, '')) LIKE ?)
		)`, pattern, pattern, pattern, pattern)
	}
}

// WithLangPattern filters by language pattern (ILIKE)
func WithLangPattern(pattern string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if pattern == "" {
			return db
		}
		return db.Where("LOWER(books.lang) LIKE ?", "%"+strings.ToLower(pattern)+"%")
	}
}

// WithGenrePattern filters by genre name pattern (ILIKE)
func WithGenrePattern(pattern string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if pattern == "" {
			return db
		}
		p := "%" + strings.ToLower(pattern) + "%"
		return db.Where(`EXISTS (
			SELECT 1 FROM bgenres bg
			JOIN genres g ON bg.genre_id = g.genre_id
			WHERE bg.book_id = books.book_id
			AND (LOWER(g.section) LIKE ? OR LOWER(g.subsection) LIKE ? OR LOWER(g.genre) LIKE ?)
		)`, p, p, p)
	}
}

// WithSeriesPattern filters by series name pattern (ILIKE)
func WithSeriesPattern(pattern string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if pattern == "" {
			return db
		}
		return db.Where(`EXISTS (
			SELECT 1 FROM bseries bs
			JOIN series s ON bs.ser_id = s.ser_id
			WHERE bs.book_id = books.book_id
			AND LOWER(s.ser) LIKE ?
		)`, "%"+strings.ToLower(pattern)+"%")
	}
}

// WithAuthorFirstNameExact filters by author first name (exact match)
func WithAuthorFirstNameExact(firstName string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if firstName == "" {
			return db
		}
		return db.Where(`EXISTS (
			SELECT 1 FROM bauthors ba
			JOIN authors a ON ba.author_id = a.author_id
			WHERE ba.book_id = books.book_id
			AND a.first_name = ?
		)`, firstName)
	}
}

// WithAuthorLastNameExact filters by author last name (exact match)
func WithAuthorLastNameExact(lastName string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if lastName == "" {
			return db
		}
		return db.Where(`EXISTS (
			SELECT 1 FROM bauthors ba
			JOIN authors a ON ba.author_id = a.author_id
			WHERE ba.book_id = books.book_id
			AND a.last_name = ?
		)`, lastName)
	}
}

// ApplyBookFilters applies all book filters
func ApplyBookFilters(filters *repository.BookFilters) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if filters == nil {
			return db.Scopes(AvailableBooks(), ExcludeDuplicates())
		}

		db = db.Scopes(WithAvailability(filters))

		if len(filters.Keywords) > 0 {
			db = db.Scopes(WithKeywords(filters.Keywords, filters.SearchInAnnotation))
		}
		if filters.AuthorNameQuery != "" {
			db = db.Scopes(WithAuthorName(filters.AuthorNameQuery))
		}
		if len(filters.AuthorIDs) > 0 {
			db = db.Scopes(WithAuthors(filters.AuthorIDs))
		}
		if len(filters.GenreIDs) > 0 {
			db = db.Scopes(WithGenres(filters.GenreIDs))
		}
		if len(filters.SeriesIDs) > 0 {
			db = db.Scopes(WithSeries(filters.SeriesIDs))
		}
		if len(filters.CatalogIDs) > 0 {
			db = db.Scopes(WithCatalogs(filters.CatalogIDs))
		}
		if len(filters.Languages) > 0 {
			db = db.Scopes(WithLanguages(filters.Languages))
		}
		if len(filters.Formats) > 0 {
			db = db.Scopes(WithFormats(filters.Formats))
		}
		// Pattern filters
		if filters.LangPattern != "" {
			db = db.Scopes(WithLangPattern(filters.LangPattern))
		}
		if filters.GenrePattern != "" {
			db = db.Scopes(WithGenrePattern(filters.GenrePattern))
		}
		if filters.SeriesPattern != "" {
			db = db.Scopes(WithSeriesPattern(filters.SeriesPattern))
		}
		// Author name exact filters
		if filters.AuthorFirstName != "" {
			db = db.Scopes(WithAuthorFirstNameExact(filters.AuthorFirstName))
		}
		if filters.AuthorLastName != "" {
			db = db.Scopes(WithAuthorLastNameExact(filters.AuthorLastName))
		}
		if filters.MinSize != nil || filters.MaxSize != nil {
			db = db.Scopes(WithSizeRange(filters.MinSize, filters.MaxSize))
		}
		if filters.RegisteredAfter != nil {
			db = db.Scopes(WithinPeriod(*filters.RegisteredAfter))
		}
		if filters.TitlePrefix != "" {
			db = db.Scopes(WithTitlePrefix(filters.TitlePrefix))
		}
		if filters.FavoritesOnly {
			db = db.Scopes(OnlyFavorites())
		}
		if filters.AudioOnly {
			db = db.Scopes(OnlyAudiobooks())
		}

		return db
	}
}

// --- Sorting Scopes ---

// ApplyBookSort applies sorting to a book query
func ApplyBookSort(sort repository.Sort) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		dir := "ASC"
		if sort.IsDescending() {
			dir = "DESC"
		}

		switch sort.Field {
		case repository.SortByTitle:
			return db.Order("books.title " + dir)
		case repository.SortByDate:
			return db.Order("books.registerdate " + dir)
		case repository.SortBySize:
			return db.Order("books.filesize " + dir)
		case repository.SortByFormat:
			return db.Order("books.format " + dir)
		case repository.SortByLanguage:
			return db.Order("books.lang " + dir)
		case repository.SortByAuthor:
			// Requires join with authors
			return db.Order("books.title " + dir) // Fallback
		case repository.SortBySeriesNumber:
			// Requires join with bseries
			return db.Order("books.title " + dir) // Fallback
		default:
			return db.Order("books.title " + dir)
		}
	}
}

// ApplyNameSort applies sorting by name field
func ApplyNameSort(sort repository.Sort, column string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		dir := "ASC"
		if sort.IsDescending() {
			dir = "DESC"
		}

		switch sort.Field {
		case repository.SortByName:
			return db.Order(column + " " + dir)
		case repository.SortByBookCount:
			return db.Order("book_count " + dir)
		default:
			return db.Order(column + " " + dir)
		}
	}
}

// --- Pagination Scopes ---

// Paginate applies pagination
func Paginate(pagination *repository.Pagination) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if pagination == nil {
			return db
		}
		if pagination.PageSize > 0 {
			db = db.Offset(pagination.Offset()).Limit(pagination.Limit())
		}
		return db
	}
}

// --- Author Scopes ---

// WithAuthorNamePrefix filters authors by last name prefix
func WithAuthorNamePrefix(prefix string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if prefix == "" {
			return db
		}
		return db.Where("UPPER(last_name) LIKE ?", strings.ToUpper(prefix)+"%")
	}
}

// WithAuthorKeywords searches in author names
func WithAuthorKeywords(keywords []string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(keywords) == 0 {
			return db
		}
		for _, kw := range keywords {
			pattern := "%" + strings.ToLower(kw) + "%"
			db = db.Where("LOWER(first_name) LIKE ? OR LOWER(last_name) LIKE ?", pattern, pattern)
		}
		return db
	}
}

// AuthorsWithBooks filters to authors who have available books
func AuthorsWithBooks() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("EXISTS (SELECT 1 FROM bauthors ba JOIN books b ON ba.book_id = b.book_id WHERE ba.author_id = authors.author_id AND b.avail != 0)")
	}
}

// ApplyAuthorFilters applies all author filters
func ApplyAuthorFilters(filters *repository.AuthorFilters) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if filters == nil {
			return db.Scopes(AuthorsWithBooks())
		}

		if filters.HasBooks {
			db = db.Scopes(AuthorsWithBooks())
		}
		if len(filters.Keywords) > 0 {
			db = db.Scopes(WithAuthorKeywords(filters.Keywords))
		}
		if filters.NamePrefix != "" {
			db = db.Scopes(WithAuthorNamePrefix(filters.NamePrefix))
		}

		return db
	}
}

// --- Series Scopes ---

// WithSeriesNamePrefix filters series by name prefix
func WithSeriesNamePrefix(prefix string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if prefix == "" {
			return db
		}
		return db.Where("UPPER(ser) LIKE ?", strings.ToUpper(prefix)+"%")
	}
}

// WithSeriesKeywords searches in series names
func WithSeriesKeywords(keywords []string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if len(keywords) == 0 {
			return db
		}
		for _, kw := range keywords {
			pattern := "%" + strings.ToLower(kw) + "%"
			db = db.Where("LOWER(ser) LIKE ?", pattern)
		}
		return db
	}
}

// SeriesWithBooks filters to series that have available books
func SeriesWithBooks() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("EXISTS (SELECT 1 FROM bseries bs JOIN books b ON bs.book_id = b.book_id WHERE bs.ser_id = series.ser_id AND b.avail != 0)")
	}
}

// ApplySeriesFilters applies all series filters
func ApplySeriesFilters(filters *repository.SeriesFilters) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if filters == nil {
			return db.Scopes(SeriesWithBooks())
		}

		if filters.HasBooks {
			db = db.Scopes(SeriesWithBooks())
		}
		if len(filters.Keywords) > 0 {
			db = db.Scopes(WithSeriesKeywords(filters.Keywords))
		}
		if filters.NamePrefix != "" {
			db = db.Scopes(WithSeriesNamePrefix(filters.NamePrefix))
		}

		return db
	}
}

// --- Genre Scopes ---

// WithGenreSection filters genres by section
func WithGenreSection(section string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if section == "" {
			return db
		}
		return db.Where("section = ?", section)
	}
}

// GenresWithBooks filters to genres that have available books
func GenresWithBooks() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("EXISTS (SELECT 1 FROM bgenres bg JOIN books b ON bg.book_id = b.book_id WHERE bg.genre_id = genres.genre_id AND b.avail != 0)")
	}
}

// ApplyGenreFilters applies all genre filters
func ApplyGenreFilters(filters *repository.GenreFilters) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if filters == nil {
			return db.Scopes(GenresWithBooks())
		}

		if filters.HasBooks {
			db = db.Scopes(GenresWithBooks())
		}
		if filters.Section != "" {
			db = db.Scopes(WithGenreSection(filters.Section))
		}

		return db
	}
}

// --- Catalog Scopes ---

// WithParentCatalog filters catalogs by parent
func WithParentCatalog(parentID *int64) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if parentID == nil {
			return db.Where("parent_id IS NULL")
		}
		return db.Where("parent_id = ?", *parentID)
	}
}

// OnlyZipCatalogs filters to only ZIP catalogs
func OnlyZipCatalogs() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("cat_type = ?", 1) // TypeZip
	}
}

// OnlyDirectoryCatalogs filters to only directory catalogs
func OnlyDirectoryCatalogs() func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("cat_type = ?", 0) // TypeNormal
	}
}

// ApplyCatalogFilters applies all catalog filters
func ApplyCatalogFilters(filters *repository.CatalogFilters) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if filters == nil {
			return db
		}

		if filters.ParentID != nil {
			db = db.Scopes(WithParentCatalog(filters.ParentID))
		}
		if filters.TypeZip != nil {
			if *filters.TypeZip {
				db = db.Scopes(OnlyZipCatalogs())
			} else {
				db = db.Scopes(OnlyDirectoryCatalogs())
			}
		}

		return db
	}
}
