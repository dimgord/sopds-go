package persistence

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/dimgord/sopds-go/internal/database"
	"github.com/dimgord/sopds-go/internal/domain/book"
	domainCatalog "github.com/dimgord/sopds-go/internal/domain/catalog"
	"github.com/dimgord/sopds-go/internal/domain/genre"
	"github.com/dimgord/sopds-go/internal/domain/repository"
	"github.com/dimgord/sopds-go/internal/domain/series"
)

// legacyDuplicateToID converts legacy *int64 duplicate to *book.ID
// Returns nil for nil (no duplicate)
func legacyDuplicateToID(d *int64) *book.ID {
	if d == nil {
		return nil
	}
	id := book.ID(*d)
	return &id
}

// Service provides a bridge between the old database API and the new repositories.
// It implements methods that match the old database.DB interface for gradual migration.
type Service struct {
	repos *Repositories
	db    *DB
}

// NewService creates a new service wrapping repositories
func NewService(db *DB) *Service {
	return &Service{
		repos: NewRepositories(db),
		db:    db,
	}
}

// Repos returns the underlying repositories for direct access
func (s *Service) Repos() *Repositories {
	return s.repos
}

// DB returns the underlying GORM database for raw queries
func (s *Service) DB() *DB {
	return s.db
}

// Transaction executes a function within a database transaction.
// All operations within fn share a single commit, reducing fsync overhead.
// Uses async commit for performance (safe for scanner - data recoverable by re-scan).
func (s *Service) Transaction(fn func(tx *Service) error) error {
	return s.db.Transaction(func(gormTx *gorm.DB) error {
		// Disable sync commit within this transaction for performance
		gormTx.Exec("SET LOCAL synchronous_commit = off")

		// Create a temporary service using the transaction
		txDB := &DB{DB: gormTx}
		txService := &Service{
			repos: NewRepositories(txDB),
			db:    txDB,
		}
		return fn(txService)
	})
}

// SetAsyncCommit disables synchronous_commit for bulk operations.
// Speeds up scans by ~10x but data may be lost on crash (recoverable by re-scan).
func (s *Service) SetAsyncCommit() error {
	return s.db.SetAsyncCommit()
}

// SetSyncCommit re-enables synchronous_commit.
func (s *Service) SetSyncCommit() error {
	return s.db.SetSyncCommit()
}

// GORM returns the raw GORM database for migrations and complex queries
func (s *Service) GORM() *gorm.DB {
	return s.db.DB
}

// Close closes the database connection
func (s *Service) Close() error {
	return s.db.Close()
}

// --- Book Operations ---

// GetBook retrieves a book by ID
func (s *Service) GetBook(ctx context.Context, id int64) (*database.Book, error) {
	b, err := s.repos.Books.FindByID(ctx, book.ID(id))
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, nil
	}
	return BookToLegacy(b), nil
}

// GetBookAuthors returns authors for a book
func (s *Service) GetBookAuthors(ctx context.Context, bookID int64) ([]database.Author, error) {
	refs, err := s.repos.Books.LoadAuthors(ctx, book.ID(bookID))
	if err != nil {
		return nil, err
	}
	return AuthorRefsToLegacy(refs), nil
}

// GetBookGenres returns genres for a book
func (s *Service) GetBookGenres(ctx context.Context, bookID int64) ([]database.Genre, error) {
	refs, err := s.repos.Books.LoadGenres(ctx, book.ID(bookID))
	if err != nil {
		return nil, err
	}
	return GenreRefsToLegacy(refs), nil
}

// GetBookSeries returns series for a book
func (s *Service) GetBookSeries(ctx context.Context, bookID int64) ([]database.BookSeries, error) {
	refs, err := s.repos.Books.LoadSeries(ctx, book.ID(bookID))
	if err != nil {
		return nil, err
	}
	return SeriesRefsToLegacy(refs), nil
}

// GetLastBooks returns recently added books
func (s *Service) GetLastBooks(ctx context.Context, limit, days int) ([]database.Book, error) {
	query := repository.NewBookQuery()
	query.Filters.WithNewPeriod(days)
	query.Sort = repository.ByDateDesc()
	query.Pagination = repository.NewPagination(0, limit)

	books, err := s.repos.Books.Find(ctx, query)
	if err != nil {
		return nil, err
	}
	return BooksToLegacy(books), nil
}

// SearchOptions contains options for book search
type SearchOptions struct {
	// Search queries (combined with AND if both set)
	TitleQuery        string // Search in title
	AuthorQuery       string // Search in author first+last name
	IncludeAnnotation bool   // Also search in annotation (for title search)

	// Scope filters (exact match by ID)
	AuthorID  int64  // Scope to author
	GenreID   int64  // Scope to genre
	SeriesID  int64  // Scope to series
	CatalogID int64  // Scope to catalog
	Lang      string // Scope to language (exact match)

	// Pattern filters (LIKE matching, combined with AND)
	LangPattern   string // Filter by language pattern (e.g., "uk", "en")
	GenrePattern  string // Filter by genre name pattern (e.g., "comedy")
	SeriesPattern string // Filter by series name pattern (e.g., "Silo")

	// Author name filters (exact match from dropdown)
	FirstNameFilter string // Filter by author first name (exact)
	LastNameFilter  string // Filter by author last name (exact)

	// Time filters
	NewPeriod int // Filter to books registered within N days (0 = disabled)

	// Type filters
	AudioOnly bool // Show only audiobooks

	ShowDuplicates bool
}

// SearchBooks searches books by keywords with optional filters
func (s *Service) SearchBooks(ctx context.Context, opts SearchOptions, pagination *database.Pagination) ([]database.Book, error) {
	query := repository.NewBookQuery()

	// Title search (with optional annotation)
	if opts.TitleQuery != "" {
		query.Filters.WithKeywords(opts.TitleQuery)
		if opts.IncludeAnnotation {
			query.Filters.IncludeAnnotation()
		}
	}

	// Author name search (combined with AND if title also set)
	if opts.AuthorQuery != "" {
		query.Filters.WithAuthorName(opts.AuthorQuery)
	}

	if opts.ShowDuplicates {
		query.Filters.IncludeDuplicates()
	}

	// Apply scope filters (exact match by ID)
	if opts.AuthorID > 0 {
		query.Filters.WithAuthors(opts.AuthorID)
	}
	if opts.GenreID > 0 {
		query.Filters.WithGenres(opts.GenreID)
	}
	if opts.SeriesID > 0 {
		query.Filters.WithSeries(opts.SeriesID)
	}
	if opts.CatalogID > 0 {
		query.Filters.WithCatalogs(opts.CatalogID)
	}
	if opts.Lang != "" {
		query.Filters.WithLanguages(book.Language(opts.Lang))
	}

	// Apply pattern filters (LIKE matching)
	if opts.LangPattern != "" {
		query.Filters.WithLangPattern(opts.LangPattern)
	}
	if opts.GenrePattern != "" {
		query.Filters.WithGenrePattern(opts.GenrePattern)
	}
	if opts.SeriesPattern != "" {
		query.Filters.WithSeriesPattern(opts.SeriesPattern)
	}

	// Apply author name filters (exact match)
	if opts.FirstNameFilter != "" {
		query.Filters.WithAuthorFirstName(opts.FirstNameFilter)
	}
	if opts.LastNameFilter != "" {
		query.Filters.WithAuthorLastName(opts.LastNameFilter)
	}

	// Time filters
	if opts.NewPeriod > 0 {
		query.Filters.WithNewPeriod(opts.NewPeriod)
		query.Sort = repository.ByDateDesc() // Sort by date for new books
	}

	// Type filters
	if opts.AudioOnly {
		query.Filters.WithAudioOnly()
	}

	query.Pagination = PaginationFromLegacy(pagination)

	books, err := s.repos.Books.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	// Update pagination with count
	count, _ := s.repos.Books.Count(ctx, query.Filters)
	if pagination != nil {
		pagination.TotalCount = count
		pagination.HasNext = int64(pagination.Offset()+len(books)) < count
		pagination.HasPrev = pagination.Page > 0
	}

	return BooksToLegacy(books), nil
}

// FilterOptions contains distinct values for filter dropdowns
type FilterOptions struct {
	Languages  []string // Distinct language codes
	FirstNames []string // Distinct author first names
	LastNames  []string // Distinct author last names
	GenreIDs   []int64  // Distinct genre IDs
	GenreNames []string // Corresponding genre names
}

// GetFilterOptions returns distinct filter values for a given search scope
func (s *Service) GetFilterOptions(ctx context.Context, opts SearchOptions) (*FilterOptions, error) {
	// Build base query with same scopes as SearchBooks but without pagination
	query := repository.NewBookQuery()

	// Apply scope filters (exact match by ID)
	if opts.AuthorID > 0 {
		query.Filters.WithAuthors(opts.AuthorID)
	}
	if opts.GenreID > 0 {
		query.Filters.WithGenres(opts.GenreID)
	}
	if opts.SeriesID > 0 {
		query.Filters.WithSeries(opts.SeriesID)
	}
	if opts.CatalogID > 0 {
		query.Filters.WithCatalogs(opts.CatalogID)
	}
	if opts.Lang != "" {
		query.Filters.WithLanguages(book.Language(opts.Lang))
	}
	if opts.NewPeriod > 0 {
		query.Filters.WithNewPeriod(opts.NewPeriod)
	}
	if opts.AudioOnly {
		query.Filters.WithAudioOnly()
	}

	db := s.db.DB

	// Get distinct languages
	var languages []string
	err := db.WithContext(ctx).
		Model(&BookModel{}).
		Scopes(ApplyBookFilters(query.Filters)).
		Distinct("lang").
		Pluck("lang", &languages).Error
	if err != nil {
		return nil, err
	}

	// Get distinct author first names
	var firstNames []string
	subQuery := db.WithContext(ctx).
		Model(&BookModel{}).
		Scopes(ApplyBookFilters(query.Filters)).
		Select("book_id")
	err = db.WithContext(ctx).
		Table("authors a").
		Joins("JOIN bauthors ba ON ba.author_id = a.author_id").
		Where("ba.book_id IN (?)", subQuery).
		Where("a.first_name IS NOT NULL AND a.first_name != ''").
		Distinct("a.first_name").
		Pluck("a.first_name", &firstNames).Error
	if err != nil {
		return nil, err
	}

	// Get distinct author last names
	var lastNames []string
	err = db.WithContext(ctx).
		Table("authors a").
		Joins("JOIN bauthors ba ON ba.author_id = a.author_id").
		Where("ba.book_id IN (?)", subQuery).
		Where("a.last_name IS NOT NULL AND a.last_name != ''").
		Distinct("a.last_name").
		Pluck("a.last_name", &lastNames).Error
	if err != nil {
		return nil, err
	}

	// Get distinct genres (only if not already scoped to a genre)
	var genreIDs []int64
	var genreNames []string
	if opts.GenreID == 0 {
		type genreResult struct {
			GenreID int64
			Name    string
		}
		var genres []genreResult
		err = db.WithContext(ctx).
			Table("genres g").
			Joins("JOIN bgenres bg ON bg.genre_id = g.genre_id").
			Where("bg.book_id IN (?)", subQuery).
			Select("DISTINCT g.genre_id, COALESCE(NULLIF(g.subsection, ''), g.genre) as name").
			Scan(&genres).Error
		if err != nil {
			return nil, err
		}
		for _, g := range genres {
			genreIDs = append(genreIDs, g.GenreID)
			genreNames = append(genreNames, g.Name)
		}
	}

	return &FilterOptions{
		Languages:  languages,
		FirstNames: firstNames,
		LastNames:  lastNames,
		GenreIDs:   genreIDs,
		GenreNames: genreNames,
	}, nil
}

// GetBooksForAuthor returns books by author
func (s *Service) GetBooksForAuthor(ctx context.Context, authorID int64, pagination *database.Pagination, showDuplicates bool) ([]database.Book, error) {
	query := repository.NewBookQuery()
	query.Filters.WithAuthors(authorID)
	if showDuplicates {
		query.Filters.IncludeDuplicates()
	}
	query.Pagination = PaginationFromLegacy(pagination)

	books, err := s.repos.Books.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	count, _ := s.repos.Books.Count(ctx, query.Filters)
	if pagination != nil {
		pagination.TotalCount = count
		pagination.HasNext = int64(pagination.Offset()+len(books)) < count
		pagination.HasPrev = pagination.Page > 0
	}

	return BooksToLegacy(books), nil
}

// GetBooksForGenre returns books by genre
func (s *Service) GetBooksForGenre(ctx context.Context, genreID int64, pagination *database.Pagination, showDuplicates bool) ([]database.Book, error) {
	query := repository.NewBookQuery()
	query.Filters.WithGenres(genreID)
	if showDuplicates {
		query.Filters.IncludeDuplicates()
	}
	query.Pagination = PaginationFromLegacy(pagination)

	books, err := s.repos.Books.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	count, _ := s.repos.Books.Count(ctx, query.Filters)
	if pagination != nil {
		pagination.TotalCount = count
		pagination.HasNext = int64(pagination.Offset()+len(books)) < count
		pagination.HasPrev = pagination.Page > 0
	}

	return BooksToLegacy(books), nil
}

// GetBooksForSeries returns books by series
func (s *Service) GetBooksForSeries(ctx context.Context, seriesID int64, pagination *database.Pagination, showDuplicates bool) ([]database.Book, error) {
	query := repository.NewBookQuery()
	query.Filters.WithSeries(seriesID)
	if showDuplicates {
		query.Filters.IncludeDuplicates()
	}
	query.Sort = repository.Sort{Field: repository.SortBySeriesNumber, Direction: repository.SortAsc}
	query.Pagination = PaginationFromLegacy(pagination)

	books, err := s.repos.Books.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	count, _ := s.repos.Books.Count(ctx, query.Filters)
	if pagination != nil {
		pagination.TotalCount = count
		pagination.HasNext = int64(pagination.Offset()+len(books)) < count
		pagination.HasPrev = pagination.Page > 0
	}

	return BooksToLegacy(books), nil
}

// GetBooksForTitle returns books starting with letter
func (s *Service) GetBooksForTitle(ctx context.Context, letter string, pagination *database.Pagination, showDuplicates bool, langID int64) ([]database.Book, error) {
	query := repository.NewBookQuery()
	query.Filters.WithTitlePrefix(letter)
	if showDuplicates {
		query.Filters.IncludeDuplicates()
	}
	query.Pagination = PaginationFromLegacy(pagination)

	books, err := s.repos.Books.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	count, _ := s.repos.Books.Count(ctx, query.Filters)
	if pagination != nil {
		pagination.TotalCount = count
		pagination.HasNext = int64(pagination.Offset()+len(books)) < count
		pagination.HasPrev = pagination.Page > 0
	}

	return BooksToLegacy(books), nil
}

// GetDuplicates returns all duplicates of a book
func (s *Service) GetDuplicates(ctx context.Context, bookID int64, pagination *database.Pagination) ([]database.Book, error) {
	repoPagination := PaginationFromLegacy(pagination)
	books, err := s.repos.Books.FindDuplicates(ctx, book.ID(bookID), repoPagination)
	if err != nil {
		return nil, err
	}
	return BooksToLegacy(books), nil
}

// CountDuplicates returns the count of duplicates for a book
func (s *Service) CountDuplicates(ctx context.Context, bookID int64) (int, error) {
	return s.repos.Books.CountDuplicates(ctx, book.ID(bookID))
}

// --- Author Operations ---

// GetAuthorsByLetter returns authors starting with letter
func (s *Service) GetAuthorsByLetter(ctx context.Context, letter string, pagination *database.Pagination) ([]database.Author, error) {
	query := repository.NewAuthorQuery()
	query.Filters.WithNamePrefix(letter)
	query.Pagination = PaginationFromLegacy(pagination)

	authors, err := s.repos.Authors.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	count, _ := s.repos.Authors.Count(ctx, query.Filters)
	if pagination != nil {
		pagination.TotalCount = count
		pagination.HasNext = int64(pagination.Offset()+len(authors)) < count
		pagination.HasPrev = pagination.Page > 0
	}

	return AuthorsToLegacy(authors), nil
}

// GetAuthorPrefixes returns author name prefixes for hierarchical navigation
func (s *Service) GetAuthorPrefixes(ctx context.Context, length int, parentPrefix string) ([]string, error) {
	return s.repos.Authors.GetPrefixes(ctx, length, parentPrefix)
}

// --- Genre Operations ---

// GetGenreSections returns distinct genre sections
func (s *Service) GetGenreSections(ctx context.Context) ([]string, error) {
	return s.repos.Genres.GetSections(ctx)
}

// GetGenresInSection returns genres in a section
func (s *Service) GetGenresInSection(ctx context.Context, section string) ([]database.Genre, error) {
	genres, err := s.repos.Genres.FindBySection(ctx, section, repository.ByName())
	if err != nil {
		return nil, err
	}
	return GenresToLegacy(genres), nil
}

// --- Series Operations ---

// GetSeriesByLetter returns series starting with letter
func (s *Service) GetSeriesByLetter(ctx context.Context, letter string, pagination *database.Pagination) ([]database.Series, error) {
	query := repository.NewSeriesQuery()
	query.Filters.WithNamePrefix(letter)
	query.Pagination = PaginationFromLegacy(pagination)

	seriesList, err := s.repos.Series.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	count, _ := s.repos.Series.Count(ctx, query.Filters)
	if pagination != nil {
		pagination.TotalCount = count
		pagination.HasNext = int64(pagination.Offset()+len(seriesList)) < count
		pagination.HasPrev = pagination.Page > 0
	}

	return SeriesSliceToLegacy(seriesList), nil
}

// GetSeriesPrefixes returns series name prefixes for hierarchical navigation
func (s *Service) GetSeriesPrefixes(ctx context.Context, length int, parentPrefix string) ([]string, error) {
	return s.repos.Series.GetPrefixes(ctx, length, parentPrefix)
}

// --- Catalog Operations ---

// GetCatalog retrieves a catalog by ID
func (s *Service) GetCatalog(ctx context.Context, id int64) (*database.Catalog, error) {
	cat, err := s.repos.Catalogs.FindByID(ctx, domainCatalog.ID(id))
	if err != nil {
		return nil, err
	}
	if cat == nil {
		return nil, nil
	}
	return CatalogToLegacy(cat), nil
}

// GetItemsInCatalog returns items in a catalog (books and subcatalogs)
func (s *Service) GetItemsInCatalog(ctx context.Context, catID int64, pagination *database.Pagination, showDuplicates bool) ([]database.CatalogItem, error) {
	var bookFilters *repository.BookFilters
	if showDuplicates {
		bookFilters = repository.NewBookFilters()
		bookFilters.IncludeDuplicates()
	}

	repoPagination := PaginationFromLegacy(pagination)

	items, err := s.repos.Catalogs.GetItems(ctx, domainCatalog.ID(catID), bookFilters, repository.ByName(), repoPagination)
	if err != nil {
		return nil, err
	}

	return CatalogItemsToLegacy(items), nil
}

// --- Bookshelf Operations ---

// AddBookShelf adds a book to user's bookshelf
func (s *Service) AddBookShelf(ctx context.Context, username string, bookID int64) error {
	return s.repos.Bookshelf.Add(ctx, username, book.ID(bookID))
}

// RemoveBookShelf removes a book from user's bookshelf
func (s *Service) RemoveBookShelf(ctx context.Context, username string, bookID int64) error {
	return s.repos.Bookshelf.Remove(ctx, username, book.ID(bookID))
}

// GetBookShelf returns books from user's bookshelf
func (s *Service) GetBookShelf(ctx context.Context, username string, pagination *database.Pagination) ([]database.Book, error) {
	repoPagination := PaginationFromLegacy(pagination)
	books, err := s.repos.Bookshelf.GetBooks(ctx, username, nil, repository.ByDateDesc(), repoPagination)
	if err != nil {
		return nil, err
	}
	return BooksToLegacy(books), nil
}

// CountBookShelf returns the count of books in user's bookshelf
func (s *Service) CountBookShelf(ctx context.Context, username string) (int64, error) {
	return s.repos.Bookshelf.Count(ctx, username)
}

// GetBookShelfIDs returns all book IDs on user's bookshelf as a set
func (s *Service) GetBookShelfIDs(ctx context.Context, username string) (map[int64]bool, error) {
	return s.repos.Bookshelf.GetBookIDs(ctx, username)
}

// MigrateAnonBookshelf copies anonymous bookshelf items to user's account
// Items that already exist in user's bookshelf are not overwritten
func (s *Service) MigrateAnonBookshelf(ctx context.Context, anonID string, userID int64) error {
	// Get anonymous bookshelf items
	var anonItems []BookshelfModel
	if err := s.db.DB.WithContext(ctx).
		Where("user_name = ?", anonID).
		Find(&anonItems).Error; err != nil {
		return err
	}

	if len(anonItems) == 0 {
		return nil
	}

	// Get user's current bookshelf to avoid duplicates
	username := fmt.Sprintf("user_%d", userID) // User's bookshelf key
	userBookIDs, err := s.repos.Bookshelf.GetBookIDs(ctx, username)
	if err != nil {
		return err
	}

	// Copy items that don't exist in user's bookshelf
	for _, item := range anonItems {
		if !userBookIDs[item.BookID] {
			newItem := BookshelfModel{
				UserName: username,
				BookID:   item.BookID,
				UserID:   &userID,
				ReadTime: item.ReadTime,
			}
			s.db.DB.WithContext(ctx).Create(&newItem)
		}
	}

	// Optionally delete anonymous items after migration
	s.db.DB.WithContext(ctx).Where("user_name = ?", anonID).Delete(&BookshelfModel{})

	return nil
}

// --- Statistics Operations ---

// GetDBInfo returns database statistics
func (s *Service) GetDBInfo(ctx context.Context, includeDetails bool) (*database.DBInfo, error) {
	booksCount, _ := s.repos.Books.Count(ctx, nil)
	authorsCount, _ := s.repos.Authors.Count(ctx, nil)
	genresCount, _ := s.repos.Genres.Count(ctx, nil)
	seriesCount, _ := s.repos.Series.Count(ctx, nil)
	catalogsCount, _ := s.repos.Catalogs.Count(ctx, nil)

	return &database.DBInfo{
		BooksCount:    booksCount,
		AuthorsCount:  authorsCount,
		GenresCount:   genresCount,
		SeriesCount:   seriesCount,
		CatalogsCount: catalogsCount,
	}, nil
}

// GetNewInfo returns new content statistics
func (s *Service) GetNewInfo(ctx context.Context, days int) (*database.NewInfo, error) {
	filters := repository.NewBookFilters()
	filters.WithNewPeriod(days)

	newBooks, _ := s.repos.Books.Count(ctx, filters)

	return &database.NewInfo{
		NewBooks: newBooks,
	}, nil
}

// GetLanguageStats returns language statistics
func (s *Service) GetLanguageStats(ctx context.Context) ([]database.LanguageInfo, error) {
	stats, err := s.repos.Books.GetLanguageStats(ctx)
	if err != nil {
		return nil, err
	}
	return LanguageStatsToLegacy(stats), nil
}

// GetBooksForLanguage returns books in a language
func (s *Service) GetBooksForLanguage(ctx context.Context, lang string, pagination *database.Pagination, showDuplicates bool) ([]database.Book, error) {
	query := repository.NewBookQuery()
	query.Filters.WithLanguages(book.Language(lang))
	if showDuplicates {
		query.Filters.IncludeDuplicates()
	}
	query.Pagination = PaginationFromLegacy(pagination)

	books, err := s.repos.Books.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	count, _ := s.repos.Books.Count(ctx, query.Filters)
	if pagination != nil {
		pagination.TotalCount = count
		pagination.HasNext = int64(pagination.Offset()+len(books)) < count
		pagination.HasPrev = pagination.Page > 0
	}

	return BooksToLegacy(books), nil
}

// --- Duplicate Management ---

// MarkDuplicates runs duplicate detection
func (s *Service) MarkDuplicates(ctx context.Context, mode database.DuplicateMode) error {
	return s.repos.Books.MarkDuplicates(ctx, repository.DuplicateMode(mode))
}

// MarkDuplicatesIncremental runs duplicate detection only for newly added books
func (s *Service) MarkDuplicatesIncremental(ctx context.Context, mode database.DuplicateMode, newBookIDs []int64, progressFn func(processed, total int)) error {
	return s.repos.Books.MarkDuplicatesIncremental(ctx, repository.DuplicateMode(mode), newBookIDs, progressFn)
}

// GetBookDuplicates returns all duplicates of a book
func (s *Service) GetBookDuplicates(ctx context.Context, bookID int64, pagination *database.Pagination) ([]database.Book, error) {
	return s.GetDuplicates(ctx, bookID, pagination)
}

// GetDuplicateCount returns the number of duplicates for a book
func (s *Service) GetDuplicateCount(ctx context.Context, bookID int64) (int, error) {
	return s.CountDuplicates(ctx, bookID)
}

// --- Genre Operations (Additional) ---

// GetGenre returns a genre by ID
func (s *Service) GetGenre(ctx context.Context, genreID int64) (*database.Genre, error) {
	g, err := s.repos.Genres.FindByID(ctx, genre.ID(genreID))
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, nil
	}
	return GenreToLegacy(g), nil
}

// --- Author Operations (Additional) ---

// GetAuthorsByPrefix returns authors by name prefix (for hierarchical navigation)
func (s *Service) GetAuthorsByPrefix(ctx context.Context, prefix string, pagination *database.Pagination) ([]database.Author, error) {
	query := repository.NewAuthorQuery()
	query.Filters.WithNamePrefix(prefix)
	query.Pagination = PaginationFromLegacy(pagination)

	authors, err := s.repos.Authors.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	count, _ := s.repos.Authors.Count(ctx, query.Filters)
	if pagination != nil {
		pagination.TotalCount = count
		pagination.HasNext = int64(pagination.Offset()+len(authors)) < count
		pagination.HasPrev = pagination.Page > 0
	}

	return AuthorsToLegacy(authors), nil
}

// --- Series Operations (Additional) ---

// GetSeries returns a series by ID
func (s *Service) GetSeries(ctx context.Context, seriesID int64) (*database.Series, error) {
	ser, err := s.repos.Series.FindByID(ctx, series.ID(seriesID))
	if err != nil {
		return nil, err
	}
	if ser == nil {
		return nil, nil
	}
	return SeriesToLegacy(ser), nil
}

// GetSeriesByPrefix returns series by name prefix (for hierarchical navigation)
func (s *Service) GetSeriesByPrefix(ctx context.Context, prefix string, pagination *database.Pagination) ([]database.Series, error) {
	query := repository.NewSeriesQuery()
	query.Filters.WithNamePrefix(prefix)
	query.Pagination = PaginationFromLegacy(pagination)

	seriesList, err := s.repos.Series.Find(ctx, query)
	if err != nil {
		return nil, err
	}

	count, _ := s.repos.Series.Count(ctx, query.Filters)
	if pagination != nil {
		pagination.TotalCount = count
		pagination.HasNext = int64(pagination.Offset()+len(seriesList)) < count
		pagination.HasPrev = pagination.Page > 0
	}

	return SeriesSliceToLegacy(seriesList), nil
}

// --- Language Operations ---

// GetLanguages returns all languages with book counts
func (s *Service) GetLanguages(ctx context.Context) ([]database.LanguageInfo, error) {
	return s.GetLanguageStats(ctx)
}

// --- Prefix Query Operations (for hierarchical navigation) ---

// GetAuthorPrefixesFiltered returns author name prefixes with parent filter
func (s *Service) GetAuthorPrefixesFiltered(ctx context.Context, parentPrefix string, length int) ([]string, error) {
	return s.repos.Authors.GetPrefixes(ctx, length, parentPrefix)
}

// CountAuthorsByPrefixQuery counts authors matching a prefix
func (s *Service) CountAuthorsByPrefixQuery(ctx context.Context, prefix string) (int64, error) {
	filters := repository.NewAuthorFilters()
	filters.WithNamePrefix(prefix)
	return s.repos.Authors.Count(ctx, filters)
}

// GetSeriesPrefixesFiltered returns series name prefixes with parent filter
func (s *Service) GetSeriesPrefixesFiltered(ctx context.Context, parentPrefix string, length int) ([]string, error) {
	return s.repos.Series.GetPrefixes(ctx, length, parentPrefix)
}

// CountSeriesByPrefixQuery counts series matching a prefix
func (s *Service) CountSeriesByPrefixQuery(ctx context.Context, prefix string) (int64, error) {
	filters := repository.NewSeriesFilters()
	filters.WithNamePrefix(prefix)
	return s.repos.Series.Count(ctx, filters)
}

// --- Scanner Operations ---

// FindBook finds a book by filename and path
func (s *Service) FindBook(ctx context.Context, filename, path string) (*database.Book, error) {
	b, err := s.repos.Books.FindByFilenameAndPath(ctx, filename, path)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, nil
	}
	return BookToLegacy(b), nil
}

// GetBookMapByCatalog returns a map of "path/filename" -> book_id for all books in a catalog.
// Used for efficient existence checking during ZIP scans (1 query instead of N).
func (s *Service) GetBookMapByCatalog(ctx context.Context, catalogID int64) (map[string]int64, error) {
	return s.repos.Books.GetBookMapByCatalog(ctx, catalogID)
}

// AddBook inserts a new book and returns its ID
func (s *Service) AddBook(ctx context.Context, b *database.Book) (int64, error) {
	// Convert legacy book to domain book
	domainBook := book.Reconstruct(
		book.ID(b.ID),
		b.Filename,
		b.Path,
		book.ParseFormat(b.Format),
		b.Filesize,
		b.Title,
		b.Annotation,
		b.DocDate,
		book.Language(b.Lang),
		b.RegisterDate,
		b.CatID,
		book.CatalogType(b.CatType),
		book.Availability(b.Avail),
		legacyDuplicateToID(b.DuplicateOf),
		book.NewCover(b.Cover, b.CoverType),
		b.Favorite,
		b.IsAudiobook,
		b.DurationSeconds,
		b.Bitrate,
		b.TrackCount,
		b.Chapters,
	)

	if err := s.repos.Books.Save(ctx, domainBook); err != nil {
		return 0, err
	}

	return int64(domainBook.ID()), nil
}

// AddBooksBatch inserts multiple books in a single transaction
func (s *Service) AddBooksBatch(ctx context.Context, books []*database.Book) ([]int64, error) {
	if len(books) == 0 {
		return nil, nil
	}

	domainBooks := make([]*book.Book, len(books))
	for i, b := range books {
		domainBooks[i] = book.Reconstruct(
			book.ID(b.ID),
			b.Filename,
			b.Path,
			book.ParseFormat(b.Format),
			b.Filesize,
			b.Title,
			b.Annotation,
			b.DocDate,
			book.Language(b.Lang),
			b.RegisterDate,
			b.CatID,
			book.CatalogType(b.CatType),
			book.Availability(b.Avail),
			legacyDuplicateToID(b.DuplicateOf),
			book.NewCover(b.Cover, b.CoverType),
			b.Favorite,
			b.IsAudiobook,
			b.DurationSeconds,
			b.Bitrate,
			b.TrackCount,
			b.Chapters,
		)
	}

	ids, err := s.repos.Books.SaveBatch(ctx, domainBooks)
	if err != nil {
		return nil, err
	}

	result := make([]int64, len(ids))
	for i, id := range ids {
		result[i] = int64(id)
	}
	return result, nil
}

// UpdateBookAvail updates a book's availability status
func (s *Service) UpdateBookAvail(ctx context.Context, bookID int64, avail database.Avail) error {
	return s.repos.Books.UpdateAvailability(ctx, book.ID(bookID), book.Availability(avail))
}

// UpdateBookAvailBatch updates availability for multiple books in a single query
func (s *Service) UpdateBookAvailBatch(ctx context.Context, bookIDs []int64, avail database.Avail) error {
	return s.repos.Books.UpdateAvailabilityBatch(ctx, bookIDs, book.Availability(avail))
}

// AddBookAuthor links a book to an author
func (s *Service) AddBookAuthor(ctx context.Context, bookID, authorID int64) error {
	return s.repos.Books.AddAuthor(ctx, book.ID(bookID), authorID)
}

// AddBookAuthorsBatch links multiple book-author pairs
func (s *Service) AddBookAuthorsBatch(ctx context.Context, pairs [][2]int64) error {
	return s.repos.Books.AddAuthorsBatch(ctx, pairs)
}

// AddBookAuthorWithRole links a book to an author with a specific role
func (s *Service) AddBookAuthorWithRole(ctx context.Context, bookID, authorID int64, role string) error {
	return s.repos.Books.AddAuthorWithRole(ctx, book.ID(bookID), authorID, role)
}

// GetBookNarrators returns narrators for a book (audiobook)
func (s *Service) GetBookNarrators(ctx context.Context, bookID int64) ([]database.Author, error) {
	refs, err := s.repos.Books.LoadNarrators(ctx, book.ID(bookID))
	if err != nil {
		return nil, err
	}
	return AuthorRefsToLegacy(refs), nil
}

// AddBookGenre links a book to a genre
func (s *Service) AddBookGenre(ctx context.Context, bookID, genreID int64) error {
	return s.repos.Books.AddGenre(ctx, book.ID(bookID), genreID)
}

// AddBookSeries links a book to a series
func (s *Service) AddBookSeries(ctx context.Context, bookID, seriesID int64, serNo int) error {
	return s.repos.Books.AddSeries(ctx, book.ID(bookID), seriesID, serNo)
}

// GetOrCreateCatalogTree creates catalog tree from path parts
func (s *Service) GetOrCreateCatalogTree(ctx context.Context, pathParts []string, catType database.CatType) (int64, error) {
	id, err := s.repos.Catalogs.GetOrCreateTree(ctx, pathParts, domainCatalog.Type(catType))
	return int64(id), err
}

// GetAllZipCatalogs returns all ZIP catalogs as path->ID map
func (s *Service) GetAllZipCatalogs(ctx context.Context) (map[string]int64, error) {
	catalogMap, err := s.repos.Catalogs.GetAllZipCatalogs(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]int64, len(catalogMap))
	for path, id := range catalogMap {
		result[path] = int64(id)
	}
	return result, nil
}

// DeleteBooksInCatalogs deletes all books in the specified catalogs
func (s *Service) DeleteBooksInCatalogs(ctx context.Context, catalogIDs []int64) (int64, error) {
	return s.repos.Books.DeleteInCatalogs(ctx, catalogIDs)
}

// RegularFileInfo contains basic info for regular file existence check
type RegularFileInfo struct {
	ID       int64
	Path     string
	Filename string
}

// GetRegularFileBooks returns all available regular files (cat_type=0) for existence check
func (s *Service) GetRegularFileBooks(ctx context.Context) ([]RegularFileInfo, error) {
	var results []RegularFileInfo
	err := s.db.DB.WithContext(ctx).
		Model(&BookModel{}).
		Select("book_id, path, filename").
		Where("cat_type = ? AND avail != ?", 0, 0).
		Find(&results).Error
	return results, err
}

// MarkBooksUnavailable marks the specified books as unavailable (avail=0)
func (s *Service) MarkBooksUnavailable(ctx context.Context, bookIDs []int64) (int64, error) {
	if len(bookIDs) == 0 {
		return 0, nil
	}
	result := s.db.DB.WithContext(ctx).
		Model(&BookModel{}).
		Where("book_id IN ?", bookIDs).
		Update("avail", 0)
	return result.RowsAffected, result.Error
}

// MarkAudioFilesInFolderDeleted marks individual audio files in a folder as unavailable.
// This is used when creating a grouped audiobook entry to clean up old individual entries.
func (s *Service) MarkAudioFilesInFolderDeleted(ctx context.Context, folderPath string) (int64, error) {
	if folderPath == "" {
		return 0, nil
	}

	// All audio formats that can be grouped
	audioFormats := []string{"mp3", "m4a", "m4b", "flac", "ogg", "opus"}

	// Mark individual audio files in this folder as unavailable
	// Files have path = folderPath and format in audioFormats
	result := s.db.DB.WithContext(ctx).
		Model(&BookModel{}).
		Where("path = ? AND format IN ? AND avail != 0", folderPath, audioFormats).
		Update("avail", 0)

	return result.RowsAffected, result.Error
}

// GetOrCreateAuthor finds an existing author or creates a new one
func (s *Service) GetOrCreateAuthor(ctx context.Context, firstName, lastName string) (*database.Author, error) {
	a, err := s.repos.Authors.GetOrCreate(ctx, firstName, lastName)
	if err != nil {
		return nil, err
	}
	return AuthorToLegacy(a), nil
}

// GetOrCreateGenre finds an existing genre or creates a new one
func (s *Service) GetOrCreateGenre(ctx context.Context, code, section, subsection string) (*database.Genre, error) {
	// Try to find by code first
	g, err := s.repos.Genres.FindByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if g != nil {
		return GenreToLegacy(g), nil
	}

	// Create new genre using raw GORM
	model := &GenreModel{
		Genre:      code,
		Section:    section,
		Subsection: subsection,
	}
	if err := s.db.DB.WithContext(ctx).Create(model).Error; err != nil {
		// Race condition - try to find again
		g, err = s.repos.Genres.FindByCode(ctx, code)
		if err != nil || g == nil {
			return nil, err
		}
		return GenreToLegacy(g), nil
	}

	return &database.Genre{
		ID:         model.ID,
		Genre:      model.Genre,
		Section:    model.Section,
		Subsection: model.Subsection,
	}, nil
}

// GetOrCreateSeries finds an existing series or creates a new one
func (s *Service) GetOrCreateSeries(ctx context.Context, name string) (*database.Series, error) {
	ser, err := s.repos.Series.GetOrCreate(ctx, name)
	if err != nil {
		return nil, err
	}
	return SeriesToLegacy(ser), nil
}

