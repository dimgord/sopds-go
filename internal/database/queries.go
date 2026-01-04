package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// FindBook finds a book by filename and path
func (db *DB) FindBook(ctx context.Context, filename, path string) (*Book, error) {
	var book Book
	err := db.pool.QueryRow(ctx, `
		SELECT book_id, filename, path, format, filesize, title, annotation,
		       lang, docdate, registerdate, cat_id, cat_type, avail, doublicat,
		       cover, cover_type, favorite
		FROM books
		WHERE filename = $1 AND path = $2
	`, filename, path).Scan(
		&book.ID, &book.Filename, &book.Path, &book.Format, &book.Filesize,
		&book.Title, &book.Annotation, &book.Lang, &book.DocDate,
		&book.RegisterDate, &book.CatID, &book.CatType, &book.Avail,
		&book.Duplicate, &book.Cover, &book.CoverType, &book.Favorite,
	)
	if err != nil {
		return nil, err
	}
	return &book, nil
}

// GetBook retrieves a book by ID
func (db *DB) GetBook(ctx context.Context, bookID int64) (*Book, error) {
	var book Book
	err := db.pool.QueryRow(ctx, `
		SELECT book_id, filename, path, format, filesize, title, annotation,
		       lang, docdate, registerdate, cat_id, cat_type, avail, doublicat,
		       cover, cover_type, favorite
		FROM books
		WHERE book_id = $1
	`, bookID).Scan(
		&book.ID, &book.Filename, &book.Path, &book.Format, &book.Filesize,
		&book.Title, &book.Annotation, &book.Lang, &book.DocDate,
		&book.RegisterDate, &book.CatID, &book.CatType, &book.Avail,
		&book.Duplicate, &book.Cover, &book.CoverType, &book.Favorite,
	)
	if err != nil {
		return nil, err
	}
	return &book, nil
}

// AddBook inserts a new book
func (db *DB) AddBook(ctx context.Context, book *Book) (int64, error) {
	var id int64
	err := db.pool.QueryRow(ctx, `
		INSERT INTO books (filename, path, format, filesize, title, annotation,
		                   lang, docdate, cat_id, cat_type, avail, doublicat,
		                   cover, cover_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING book_id
	`, book.Filename, book.Path, book.Format, book.Filesize, book.Title,
		book.Annotation, book.Lang, book.DocDate, book.CatID, book.CatType,
		book.Avail, book.Duplicate, book.Cover, book.CoverType,
	).Scan(&id)
	return id, err
}

// AddBooksBatch inserts multiple books in a single batch operation
func (db *DB) AddBooksBatch(ctx context.Context, books []*Book) ([]int64, error) {
	if len(books) == 0 {
		return nil, nil
	}

	batch := &pgx.Batch{}
	for _, book := range books {
		batch.Queue(`
			INSERT INTO books (filename, path, format, filesize, title, annotation,
			                   lang, docdate, cat_id, cat_type, avail, doublicat,
			                   cover, cover_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			RETURNING book_id
		`, book.Filename, book.Path, book.Format, book.Filesize, book.Title,
			book.Annotation, book.Lang, book.DocDate, book.CatID, book.CatType,
			book.Avail, book.Duplicate, book.Cover, book.CoverType)
	}

	br := db.pool.SendBatch(ctx, batch)
	defer br.Close()

	ids := make([]int64, len(books))
	for i := range books {
		var id int64
		if err := br.QueryRow().Scan(&id); err != nil {
			return nil, err
		}
		ids[i] = id
	}

	return ids, nil
}

// AddBookAuthorsBatch inserts multiple book-author links in a single batch
func (db *DB) AddBookAuthorsBatch(ctx context.Context, bookAuthorPairs [][2]int64) error {
	if len(bookAuthorPairs) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, pair := range bookAuthorPairs {
		batch.Queue(`
			INSERT INTO bauthors (book_id, author_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, pair[0], pair[1])
	}

	br := db.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range bookAuthorPairs {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}

	return nil
}

// UpdateBookAvail updates the availability status of a book
func (db *DB) UpdateBookAvail(ctx context.Context, bookID int64, avail Avail) error {
	_, err := db.pool.Exec(ctx,
		"UPDATE books SET avail = $1 WHERE book_id = $2",
		avail, bookID,
	)
	return err
}

// MarkAllPending marks all books as pending verification
func (db *DB) MarkAllPending(ctx context.Context) error {
	_, err := db.pool.Exec(ctx, "UPDATE books SET avail = $1 WHERE avail = $2", AvailPending, AvailVerified)
	return err
}

// MarkCatalogVerified marks all books in a catalog as verified
func (db *DB) MarkCatalogVerified(ctx context.Context, catID int64) error {
	_, err := db.pool.Exec(ctx, "UPDATE books SET avail = $1 WHERE cat_id = $2 AND avail = $3", AvailVerified, catID, AvailPending)
	return err
}

// MarkCatalogsVerified marks all books in multiple catalogs as verified in one query
func (db *DB) MarkCatalogsVerified(ctx context.Context, catIDs []int64) error {
	if len(catIDs) == 0 {
		return nil
	}
	_, err := db.pool.Exec(ctx,
		"UPDATE books SET avail = $1 WHERE cat_id = ANY($2) AND avail = $3",
		AvailVerified, catIDs, AvailPending)
	return err
}

// DeleteBooksInCatalogs marks all books in specified catalogs as deleted
func (db *DB) DeleteBooksInCatalogs(ctx context.Context, catIDs []int64) (int64, error) {
	if len(catIDs) == 0 {
		return 0, nil
	}

	// Mark books as deleted
	result, err := db.pool.Exec(ctx,
		"UPDATE books SET avail = $1 WHERE cat_id = ANY($2)",
		AvailDeleted, catIDs)
	if err != nil {
		return 0, err
	}
	count := result.RowsAffected()

	// Delete the catalog entries
	_, err = db.pool.Exec(ctx, "DELETE FROM catalogs WHERE cat_id = ANY($1)", catIDs)
	if err != nil {
		return count, err
	}

	return count, nil
}

// DeleteUnavailable deletes books that are no longer available
func (db *DB) DeleteUnavailable(ctx context.Context, logical bool) (int64, error) {
	if logical {
		result, err := db.pool.Exec(ctx, "UPDATE books SET avail = $1 WHERE avail = $2", AvailDeleted, AvailPending)
		if err != nil {
			return 0, err
		}
		return result.RowsAffected(), nil
	}

	// Physical delete with cascade
	result, err := db.pool.Exec(ctx, `
		DELETE FROM bauthors WHERE book_id IN (SELECT book_id FROM books WHERE avail = $1);
		DELETE FROM bgenres WHERE book_id IN (SELECT book_id FROM books WHERE avail = $1);
		DELETE FROM bseries WHERE book_id IN (SELECT book_id FROM books WHERE avail = $1);
		DELETE FROM bookshelf WHERE book_id IN (SELECT book_id FROM books WHERE avail = $1);
		DELETE FROM books WHERE avail = $1
	`, AvailPending)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// FindAuthor finds an author by name
func (db *DB) FindAuthor(ctx context.Context, firstName, lastName string) (*Author, error) {
	var author Author
	err := db.pool.QueryRow(ctx, `
		SELECT author_id, first_name, last_name
		FROM authors
		WHERE first_name = $1 AND last_name = $2
	`, firstName, lastName).Scan(&author.ID, &author.FirstName, &author.LastName)
	if err != nil {
		return nil, err
	}
	return &author, nil
}

// AddAuthor inserts a new author
func (db *DB) AddAuthor(ctx context.Context, firstName, lastName string) (int64, error) {
	var id int64
	err := db.pool.QueryRow(ctx, `
		INSERT INTO authors (first_name, last_name)
		VALUES ($1, $2)
		RETURNING author_id
	`, firstName, lastName).Scan(&id)
	return id, err
}

// GetOrCreateAuthor finds or creates an author
func (db *DB) GetOrCreateAuthor(ctx context.Context, firstName, lastName string) (int64, error) {
	author, err := db.FindAuthor(ctx, firstName, lastName)
	if err == nil {
		return author.ID, nil
	}
	return db.AddAuthor(ctx, firstName, lastName)
}

// AddBookAuthor links a book to an author
func (db *DB) AddBookAuthor(ctx context.Context, bookID, authorID int64) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO bauthors (book_id, author_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, bookID, authorID)
	return err
}

// GetBookAuthors gets all authors for a book
func (db *DB) GetBookAuthors(ctx context.Context, bookID int64) ([]Author, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT a.author_id, a.first_name, a.last_name
		FROM authors a
		JOIN bauthors ba ON a.author_id = ba.author_id
		WHERE ba.book_id = $1
		ORDER BY a.last_name, a.first_name
	`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var authors []Author
	for rows.Next() {
		var a Author
		if err := rows.Scan(&a.ID, &a.FirstName, &a.LastName); err != nil {
			return nil, err
		}
		authors = append(authors, a)
	}
	return authors, rows.Err()
}

// FindGenre finds a genre by code
func (db *DB) FindGenre(ctx context.Context, genre string) (*Genre, error) {
	var g Genre
	err := db.pool.QueryRow(ctx, `
		SELECT genre_id, genre, section, subsection
		FROM genres
		WHERE genre = $1
	`, genre).Scan(&g.ID, &g.Genre, &g.Section, &g.Subsection)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// AddBookGenre links a book to a genre
func (db *DB) AddBookGenre(ctx context.Context, bookID, genreID int64) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO bgenres (book_id, genre_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, bookID, genreID)
	return err
}

// GetBookGenres gets all genres for a book
func (db *DB) GetBookGenres(ctx context.Context, bookID int64) ([]Genre, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT g.genre_id, g.genre, g.section, g.subsection
		FROM genres g
		JOIN bgenres bg ON g.genre_id = bg.genre_id
		WHERE bg.book_id = $1
	`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var genres []Genre
	for rows.Next() {
		var g Genre
		if err := rows.Scan(&g.ID, &g.Genre, &g.Section, &g.Subsection); err != nil {
			return nil, err
		}
		genres = append(genres, g)
	}
	return genres, rows.Err()
}

// FindSeries finds a series by name
func (db *DB) FindSeries(ctx context.Context, name string) (*Series, error) {
	var s Series
	err := db.pool.QueryRow(ctx, `
		SELECT ser_id, ser
		FROM series
		WHERE ser = $1
	`, name).Scan(&s.ID, &s.Name)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// AddSeries inserts a new series
func (db *DB) AddSeries(ctx context.Context, name string) (int64, error) {
	var id int64
	err := db.pool.QueryRow(ctx, `
		INSERT INTO series (ser)
		VALUES ($1)
		RETURNING ser_id
	`, name).Scan(&id)
	return id, err
}

// GetOrCreateSeries finds or creates a series
func (db *DB) GetOrCreateSeries(ctx context.Context, name string) (int64, error) {
	series, err := db.FindSeries(ctx, name)
	if err == nil {
		return series.ID, nil
	}
	return db.AddSeries(ctx, name)
}

// AddBookSeries links a book to a series
func (db *DB) AddBookSeries(ctx context.Context, bookID, seriesID int64, serNo int) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO bseries (book_id, ser_id, ser_no)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, bookID, seriesID, serNo)
	return err
}

// GetBookSeries gets all series for a book
func (db *DB) GetBookSeries(ctx context.Context, bookID int64) ([]BookSeries, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT s.ser_id, bs.book_id, bs.ser_no, s.ser
		FROM series s
		JOIN bseries bs ON s.ser_id = bs.ser_id
		WHERE bs.book_id = $1
		ORDER BY s.ser
	`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var series []BookSeries
	for rows.Next() {
		var bs BookSeries
		if err := rows.Scan(&bs.SeriesID, &bs.BookID, &bs.SerNo, &bs.Name); err != nil {
			return nil, err
		}
		series = append(series, bs)
	}
	return series, rows.Err()
}

// FindCatalog finds a catalog by name and path
func (db *DB) FindCatalog(ctx context.Context, name, path string) (*Catalog, error) {
	var c Catalog
	err := db.pool.QueryRow(ctx, `
		SELECT cat_id, parent_id, cat_name, path, cat_type
		FROM catalogs
		WHERE cat_name = $1 AND path = $2
	`, name, path).Scan(&c.ID, &c.ParentID, &c.Name, &c.Path, &c.CatType)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// AddCatalog inserts a new catalog
func (db *DB) AddCatalog(ctx context.Context, catalog *Catalog) (int64, error) {
	var id int64
	err := db.pool.QueryRow(ctx, `
		INSERT INTO catalogs (parent_id, cat_name, path, cat_type)
		VALUES ($1, $2, $3, $4)
		RETURNING cat_id
	`, catalog.ParentID, catalog.Name, catalog.Path, catalog.CatType).Scan(&id)
	return id, err
}

// GetOrCreateCatalogTree creates catalog tree from path parts
func (db *DB) GetOrCreateCatalogTree(ctx context.Context, pathParts []string, catType CatType) (int64, error) {
	var parentID *int64
	var currentPath string

	for _, part := range pathParts {
		if currentPath == "" {
			currentPath = part
		} else {
			currentPath = currentPath + "/" + part
		}

		cat, err := db.FindCatalog(ctx, part, currentPath)
		if err == nil {
			parentID = &cat.ID
			continue
		}

		// Create new catalog
		newCat := &Catalog{
			ParentID: parentID,
			Name:     part,
			Path:     currentPath,
			CatType:  catType,
		}
		id, err := db.AddCatalog(ctx, newCat)
		if err != nil {
			return 0, err
		}
		parentID = &id
	}

	if parentID == nil {
		return 0, fmt.Errorf("empty path")
	}
	return *parentID, nil
}

// GetItemsInCatalog gets books and subcatalogs in a catalog
func (db *DB) GetItemsInCatalog(ctx context.Context, catID int64, page *Pagination, showDuplicates bool) ([]CatalogItem, error) {
	dupFilter := ""
	if !showDuplicates {
		dupFilter = "AND b.doublicat = 0"
	}

	query := fmt.Sprintf(`
		WITH items AS (
			SELECT 'catalog' as item_type, cat_id as id, cat_name as name, path,
			       CURRENT_TIMESTAMP as date, '' as title, '' as annotation,
			       '' as docdate, '' as format, 0 as filesize, '' as cover, '' as cover_type
			FROM catalogs
			WHERE parent_id = $1
			UNION ALL
			SELECT 'book' as item_type, book_id as id, filename as name, path,
			       registerdate as date, title, annotation, docdate, format,
			       filesize, cover, cover_type
			FROM books b
			WHERE cat_id = $1 AND avail != 0 %s
		)
		SELECT * FROM items
		ORDER BY item_type DESC, name
		LIMIT $2 OFFSET $3
	`, dupFilter)

	rows, err := db.pool.Query(ctx, query, catID, page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []CatalogItem
	for rows.Next() {
		var item CatalogItem
		if err := rows.Scan(
			&item.ItemType, &item.ID, &item.Name, &item.Path,
			&item.Date, &item.Title, &item.Annotation, &item.DocDate,
			&item.Format, &item.Filesize, &item.Cover, &item.CoverType,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	// Get total count
	var count int64
	err = db.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FROM (
			SELECT cat_id FROM catalogs WHERE parent_id = $1
			UNION ALL
			SELECT book_id FROM books b WHERE cat_id = $1 AND avail != 0 %s
		) t
	`, dupFilter), catID).Scan(&count)
	if err != nil {
		return nil, err
	}
	page.TotalCount = count
	page.HasNext = int64(page.Offset()+page.Limit) < count
	page.HasPrev = page.Page > 0

	return items, rows.Err()
}

// GetBooksForTitle searches books by title prefix
func (db *DB) GetBooksForTitle(ctx context.Context, prefix string, page *Pagination, showDuplicates bool, newPeriod int) ([]Book, error) {
	dupFilter := ""
	if !showDuplicates {
		dupFilter = "AND doublicat = 0"
	}

	newFilter := ""
	if newPeriod > 0 {
		newFilter = fmt.Sprintf("AND registerdate >= NOW() - INTERVAL '%d days'", newPeriod)
	}

	// Get total count first
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM books
		WHERE avail != 0 AND LOWER(title) LIKE $1 %s %s
	`, dupFilter, newFilter)
	var count int64
	if err := db.pool.QueryRow(ctx, countQuery, strings.ToLower(prefix)+"%").Scan(&count); err != nil {
		return nil, err
	}
	page.TotalCount = count
	page.HasNext = int64(page.Offset()+page.Limit) < count
	page.HasPrev = page.Page > 0

	query := fmt.Sprintf(`
		SELECT book_id, filename, path, format, filesize, title, annotation,
		       lang, docdate, registerdate, cat_id, cat_type, avail, doublicat,
		       cover, cover_type, favorite
		FROM books
		WHERE avail != 0 AND LOWER(title) LIKE $1 %s %s
		ORDER BY title
		LIMIT $2 OFFSET $3
	`, dupFilter, newFilter)

	rows, err := db.pool.Query(ctx, query, strings.ToLower(prefix)+"%", page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(
			&b.ID, &b.Filename, &b.Path, &b.Format, &b.Filesize,
			&b.Title, &b.Annotation, &b.Lang, &b.DocDate,
			&b.RegisterDate, &b.CatID, &b.CatType, &b.Avail,
			&b.Duplicate, &b.Cover, &b.CoverType, &b.Favorite,
		); err != nil {
			return nil, err
		}
		books = append(books, b)
	}

	return books, rows.Err()
}

// SearchFilters contains optional filters for search
type SearchFilters struct {
	Lang      string // Filter by language code
	FirstName string // Filter by author first name
	LastName  string // Filter by author last name
	GenreID   int64  // Filter by genre ID
	AuthorID  int64  // Filter by author ID
	SeriesID  int64  // Filter by series ID
}

// SearchBooks searches books by title, author name, or series name with optional filters
func (db *DB) SearchBooks(ctx context.Context, query string, page *Pagination, showDuplicates bool, filters *SearchFilters) ([]Book, error) {
	dupFilter := ""
	if !showDuplicates {
		dupFilter = "AND b.doublicat = 0"
	}

	// Build filter conditions
	filterConds := ""
	if filters != nil {
		if filters.Lang != "" {
			filterConds += fmt.Sprintf(" AND b.lang = '%s'", filters.Lang)
		}
		if filters.FirstName != "" {
			filterConds += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM bauthors ba2 JOIN authors a2 ON ba2.author_id = a2.author_id WHERE ba2.book_id = b.book_id AND a2.first_name = '%s')", filters.FirstName)
		}
		if filters.LastName != "" {
			filterConds += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM bauthors ba3 JOIN authors a3 ON ba3.author_id = a3.author_id WHERE ba3.book_id = b.book_id AND a3.last_name = '%s')", filters.LastName)
		}
		if filters.GenreID > 0 {
			filterConds += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM bgenres bg WHERE bg.book_id = b.book_id AND bg.genre_id = %d)", filters.GenreID)
		}
		if filters.AuthorID > 0 {
			filterConds += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM bauthors ba4 WHERE ba4.book_id = b.book_id AND ba4.author_id = %d)", filters.AuthorID)
		}
		if filters.SeriesID > 0 {
			filterConds += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM bseries bs2 WHERE bs2.book_id = b.book_id AND bs2.ser_id = %d)", filters.SeriesID)
		}
	}

	searchPattern := "%" + strings.ToLower(query) + "%"

	// Get total count first - search in title, author name, or series name
	countQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT b.book_id) FROM books b
		LEFT JOIN bauthors ba ON b.book_id = ba.book_id
		LEFT JOIN authors a ON ba.author_id = a.author_id
		LEFT JOIN bseries bs ON b.book_id = bs.book_id
		LEFT JOIN series s ON bs.ser_id = s.ser_id
		WHERE b.avail != 0 %s %s
		AND (LOWER(b.title) LIKE $1
		     OR LOWER(COALESCE(a.first_name, '')) LIKE $1
		     OR LOWER(COALESCE(a.last_name, '')) LIKE $1
		     OR LOWER(COALESCE(s.ser, '')) LIKE $1)
	`, dupFilter, filterConds)
	var count int64
	if err := db.pool.QueryRow(ctx, countQuery, searchPattern).Scan(&count); err != nil {
		return nil, err
	}
	page.TotalCount = count
	page.HasNext = int64(page.Offset()+page.Limit) < count
	page.HasPrev = page.Page > 0

	bookQuery := fmt.Sprintf(`
		SELECT DISTINCT b.book_id, b.filename, b.path, b.format, b.filesize, b.title, b.annotation,
		       b.lang, b.docdate, b.registerdate, b.cat_id, b.cat_type, b.avail, b.doublicat,
		       b.cover, b.cover_type, b.favorite
		FROM books b
		LEFT JOIN bauthors ba ON b.book_id = ba.book_id
		LEFT JOIN authors a ON ba.author_id = a.author_id
		LEFT JOIN bseries bs ON b.book_id = bs.book_id
		LEFT JOIN series s ON bs.ser_id = s.ser_id
		WHERE b.avail != 0 %s %s
		AND (LOWER(b.title) LIKE $1
		     OR LOWER(COALESCE(a.first_name, '')) LIKE $1
		     OR LOWER(COALESCE(a.last_name, '')) LIKE $1
		     OR LOWER(COALESCE(s.ser, '')) LIKE $1)
		ORDER BY b.title
		LIMIT $2 OFFSET $3
	`, dupFilter, filterConds)

	rows, err := db.pool.Query(ctx, bookQuery, searchPattern, page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(
			&b.ID, &b.Filename, &b.Path, &b.Format, &b.Filesize,
			&b.Title, &b.Annotation, &b.Lang, &b.DocDate,
			&b.RegisterDate, &b.CatID, &b.CatType, &b.Avail,
			&b.Duplicate, &b.Cover, &b.CoverType, &b.Favorite,
		); err != nil {
			return nil, err
		}
		books = append(books, b)
	}

	return books, rows.Err()
}

// GetBooksForAuthor gets books by author ID
func (db *DB) GetBooksForAuthor(ctx context.Context, authorID int64, page *Pagination, showDuplicates bool) ([]Book, error) {
	dupFilter := ""
	if !showDuplicates {
		dupFilter = "AND b.doublicat = 0"
	}

	// Get total count first
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM books b
		JOIN bauthors ba ON b.book_id = ba.book_id
		WHERE ba.author_id = $1 AND b.avail != 0 %s
	`, dupFilter)
	var count int64
	if err := db.pool.QueryRow(ctx, countQuery, authorID).Scan(&count); err != nil {
		return nil, err
	}
	page.TotalCount = count
	page.HasNext = int64(page.Offset()+page.Limit) < count
	page.HasPrev = page.Page > 0

	query := fmt.Sprintf(`
		SELECT b.book_id, b.filename, b.path, b.format, b.filesize, b.title, b.annotation,
		       b.lang, b.docdate, b.registerdate, b.cat_id, b.cat_type, b.avail, b.doublicat,
		       b.cover, b.cover_type, b.favorite
		FROM books b
		JOIN bauthors ba ON b.book_id = ba.book_id
		WHERE ba.author_id = $1 AND b.avail != 0 %s
		ORDER BY b.title
		LIMIT $2 OFFSET $3
	`, dupFilter)

	rows, err := db.pool.Query(ctx, query, authorID, page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(
			&b.ID, &b.Filename, &b.Path, &b.Format, &b.Filesize,
			&b.Title, &b.Annotation, &b.Lang, &b.DocDate,
			&b.RegisterDate, &b.CatID, &b.CatType, &b.Avail,
			&b.Duplicate, &b.Cover, &b.CoverType, &b.Favorite,
		); err != nil {
			return nil, err
		}
		books = append(books, b)
	}

	return books, rows.Err()
}

// GetBooksForGenre gets books by genre ID
func (db *DB) GetBooksForGenre(ctx context.Context, genreID int64, page *Pagination, showDuplicates bool) ([]Book, error) {
	dupFilter := ""
	if !showDuplicates {
		dupFilter = "AND b.doublicat = 0"
	}

	// Get total count first
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM books b
		JOIN bgenres bg ON b.book_id = bg.book_id
		WHERE bg.genre_id = $1 AND b.avail != 0 %s
	`, dupFilter)
	var count int64
	if err := db.pool.QueryRow(ctx, countQuery, genreID).Scan(&count); err != nil {
		return nil, err
	}
	page.TotalCount = count
	page.HasNext = int64(page.Offset()+page.Limit) < count
	page.HasPrev = page.Page > 0

	query := fmt.Sprintf(`
		SELECT b.book_id, b.filename, b.path, b.format, b.filesize, b.title, b.annotation,
		       b.lang, b.docdate, b.registerdate, b.cat_id, b.cat_type, b.avail, b.doublicat,
		       b.cover, b.cover_type, b.favorite
		FROM books b
		JOIN bgenres bg ON b.book_id = bg.book_id
		WHERE bg.genre_id = $1 AND b.avail != 0 %s
		ORDER BY b.title
		LIMIT $2 OFFSET $3
	`, dupFilter)

	rows, err := db.pool.Query(ctx, query, genreID, page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(
			&b.ID, &b.Filename, &b.Path, &b.Format, &b.Filesize,
			&b.Title, &b.Annotation, &b.Lang, &b.DocDate,
			&b.RegisterDate, &b.CatID, &b.CatType, &b.Avail,
			&b.Duplicate, &b.Cover, &b.CoverType, &b.Favorite,
		); err != nil {
			return nil, err
		}
		books = append(books, b)
	}

	return books, rows.Err()
}

// GetBooksForSeries gets books in a series
func (db *DB) GetBooksForSeries(ctx context.Context, seriesID int64, page *Pagination, showDuplicates bool) ([]Book, error) {
	dupFilter := ""
	if !showDuplicates {
		dupFilter = "AND b.doublicat = 0"
	}

	// Get total count first
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM books b
		JOIN bseries bs ON b.book_id = bs.book_id
		WHERE bs.ser_id = $1 AND b.avail != 0 %s
	`, dupFilter)
	var count int64
	if err := db.pool.QueryRow(ctx, countQuery, seriesID).Scan(&count); err != nil {
		return nil, err
	}
	page.TotalCount = count
	page.HasNext = int64(page.Offset()+page.Limit) < count
	page.HasPrev = page.Page > 0

	query := fmt.Sprintf(`
		SELECT b.book_id, b.filename, b.path, b.format, b.filesize, b.title, b.annotation,
		       b.lang, b.docdate, b.registerdate, b.cat_id, b.cat_type, b.avail, b.doublicat,
		       b.cover, b.cover_type, b.favorite
		FROM books b
		JOIN bseries bs ON b.book_id = bs.book_id
		WHERE bs.ser_id = $1 AND b.avail != 0 %s
		ORDER BY bs.ser_no, b.title
		LIMIT $2 OFFSET $3
	`, dupFilter)

	rows, err := db.pool.Query(ctx, query, seriesID, page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(
			&b.ID, &b.Filename, &b.Path, &b.Format, &b.Filesize,
			&b.Title, &b.Annotation, &b.Lang, &b.DocDate,
			&b.RegisterDate, &b.CatID, &b.CatType, &b.Avail,
			&b.Duplicate, &b.Cover, &b.CoverType, &b.Favorite,
		); err != nil {
			return nil, err
		}
		books = append(books, b)
	}

	return books, rows.Err()
}

// GetLastBooks gets recently added books
func (db *DB) GetLastBooks(ctx context.Context, limit int, newPeriod int) ([]Book, error) {
	query := `
		SELECT book_id, filename, path, format, filesize, title, annotation,
		       lang, docdate, registerdate, cat_id, cat_type, avail, doublicat,
		       cover, cover_type, favorite
		FROM books
		WHERE avail != 0 AND doublicat = 0 AND registerdate >= NOW() - $1::INTERVAL
		ORDER BY registerdate DESC
		LIMIT $2
	`

	interval := fmt.Sprintf("%d days", newPeriod)
	rows, err := db.pool.Query(ctx, query, interval, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(
			&b.ID, &b.Filename, &b.Path, &b.Format, &b.Filesize,
			&b.Title, &b.Annotation, &b.Lang, &b.DocDate,
			&b.RegisterDate, &b.CatID, &b.CatType, &b.Avail,
			&b.Duplicate, &b.Cover, &b.CoverType, &b.Favorite,
		); err != nil {
			return nil, err
		}
		books = append(books, b)
	}

	return books, rows.Err()
}

// GetAuthorsByLetter gets authors starting with given letters
func (db *DB) GetAuthorsByLetter(ctx context.Context, prefix string, page *Pagination) ([]Author, error) {
	// Get total count first
	var count int64
	if err := db.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT a.author_id)
		FROM authors a
		JOIN bauthors ba ON a.author_id = ba.author_id
		JOIN books b ON ba.book_id = b.book_id
		WHERE b.avail != 0 AND LOWER(a.last_name) LIKE $1
	`, strings.ToLower(prefix)+"%").Scan(&count); err != nil {
		return nil, err
	}
	page.TotalCount = count
	page.HasNext = int64(page.Offset()+page.Limit) < count
	page.HasPrev = page.Page > 0

	rows, err := db.pool.Query(ctx, `
		SELECT DISTINCT a.author_id, a.first_name, a.last_name
		FROM authors a
		JOIN bauthors ba ON a.author_id = ba.author_id
		JOIN books b ON ba.book_id = b.book_id
		WHERE b.avail != 0 AND LOWER(a.last_name) LIKE $1
		ORDER BY a.last_name, a.first_name
		LIMIT $2 OFFSET $3
	`, strings.ToLower(prefix)+"%", page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var authors []Author
	for rows.Next() {
		var a Author
		if err := rows.Scan(&a.ID, &a.FirstName, &a.LastName); err != nil {
			return nil, err
		}
		authors = append(authors, a)
	}

	return authors, rows.Err()
}

// GetSeriesByLetter gets series starting with given letters
func (db *DB) GetSeriesByLetter(ctx context.Context, prefix string, page *Pagination) ([]Series, error) {
	// Get total count first
	var count int64
	if err := db.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT s.ser_id)
		FROM series s
		JOIN bseries bs ON s.ser_id = bs.ser_id
		JOIN books b ON bs.book_id = b.book_id
		WHERE b.avail != 0 AND LOWER(s.ser) LIKE $1
	`, strings.ToLower(prefix)+"%").Scan(&count); err != nil {
		return nil, err
	}
	page.TotalCount = count
	page.HasNext = int64(page.Offset()+page.Limit) < count
	page.HasPrev = page.Page > 0

	rows, err := db.pool.Query(ctx, `
		SELECT DISTINCT s.ser_id, s.ser
		FROM series s
		JOIN bseries bs ON s.ser_id = bs.ser_id
		JOIN books b ON bs.book_id = b.book_id
		WHERE b.avail != 0 AND LOWER(s.ser) LIKE $1
		ORDER BY s.ser
		LIMIT $2 OFFSET $3
	`, strings.ToLower(prefix)+"%", page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var series []Series
	for rows.Next() {
		var s Series
		if err := rows.Scan(&s.ID, &s.Name); err != nil {
			return nil, err
		}
		series = append(series, s)
	}

	return series, rows.Err()
}

// GetGenre gets a genre by ID
func (db *DB) GetGenre(ctx context.Context, genreID int64) (*Genre, error) {
	var g Genre
	err := db.pool.QueryRow(ctx, `
		SELECT genre_id, genre, section, subsection
		FROM genres
		WHERE genre_id = $1
	`, genreID).Scan(&g.ID, &g.Genre, &g.Section, &g.Subsection)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// GetGenreSections gets top-level genre sections
func (db *DB) GetGenreSections(ctx context.Context) ([]string, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT DISTINCT section
		FROM genres
		ORDER BY section
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sections []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		sections = append(sections, s)
	}

	return sections, rows.Err()
}

// GetGenresInSection gets genres within a section
func (db *DB) GetGenresInSection(ctx context.Context, section string) ([]Genre, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT genre_id, genre, section, subsection
		FROM genres
		WHERE section = $1
		ORDER BY subsection
	`, section)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var genres []Genre
	for rows.Next() {
		var g Genre
		if err := rows.Scan(&g.ID, &g.Genre, &g.Section, &g.Subsection); err != nil {
			return nil, err
		}
		genres = append(genres, g)
	}

	return genres, rows.Err()
}

// GetLanguages returns all distinct languages with book counts
// GetDistinctFormats returns list of distinct book formats
func (db *DB) GetDistinctFormats(ctx context.Context) ([]string, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT DISTINCT format FROM books WHERE avail != 0 ORDER BY format
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var formats []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		formats = append(formats, f)
	}
	return formats, rows.Err()
}

// GetDistinctLanguages returns list of distinct language codes
func (db *DB) GetDistinctLanguages(ctx context.Context) ([]string, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT DISTINCT COALESCE(NULLIF(lang, ''), 'unknown') as language
		FROM books WHERE avail != 0
		ORDER BY language
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var langs []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		langs = append(langs, l)
	}
	return langs, rows.Err()
}

func (db *DB) GetLanguages(ctx context.Context) ([]LanguageInfo, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(lang, ''), 'unknown') as language, COUNT(*) as cnt
		FROM books
		WHERE avail != 0 AND doublicat = 0
		GROUP BY language
		ORDER BY cnt DESC, language
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var langs []LanguageInfo
	for rows.Next() {
		var l LanguageInfo
		if err := rows.Scan(&l.Code, &l.Count); err != nil {
			return nil, err
		}
		langs = append(langs, l)
	}

	return langs, rows.Err()
}

// GetBooksForLanguage returns books for a given language
func (db *DB) GetBooksForLanguage(ctx context.Context, lang string, pagination *Pagination, showDuplicates bool) ([]Book, error) {
	dupFilter := ""
	if !showDuplicates {
		dupFilter = "AND doublicat = 0"
	}

	var query string
	var rows interface {
		Next() bool
		Scan(dest ...any) error
		Close()
		Err() error
	}
	var err error

	if lang == "unknown" {
		query = fmt.Sprintf(`
			SELECT book_id, filename, path, format, filesize, title, annotation,
			       lang, docdate, registerdate, cat_id, cat_type, avail, doublicat,
			       cover, cover_type, favorite
			FROM books
			WHERE avail != 0 %s AND (lang = '' OR lang IS NULL)
			ORDER BY title
			LIMIT $1 OFFSET $2
		`, dupFilter)
		rows, err = db.pool.Query(ctx, query, pagination.Limit, pagination.Offset())
	} else {
		query = fmt.Sprintf(`
			SELECT book_id, filename, path, format, filesize, title, annotation,
			       lang, docdate, registerdate, cat_id, cat_type, avail, doublicat,
			       cover, cover_type, favorite
			FROM books
			WHERE avail != 0 %s AND lang = $1
			ORDER BY title
			LIMIT $2 OFFSET $3
		`, dupFilter)
		rows, err = db.pool.Query(ctx, query, lang, pagination.Limit, pagination.Offset())
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(
			&b.ID, &b.Filename, &b.Path, &b.Format, &b.Filesize,
			&b.Title, &b.Annotation, &b.Lang, &b.DocDate,
			&b.RegisterDate, &b.CatID, &b.CatType, &b.Avail,
			&b.Duplicate, &b.Cover, &b.CoverType, &b.Favorite,
		); err != nil {
			return nil, err
		}
		books = append(books, b)
	}

	return books, rows.Err()
}

// GetDBInfo gets database statistics
func (db *DB) GetDBInfo(ctx context.Context, showDuplicates bool) (*DBInfo, error) {
	dupFilter := ""
	if !showDuplicates {
		dupFilter = "AND doublicat = 0"
	}

	var info DBInfo

	err := db.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FROM books WHERE avail != 0 %s
	`, dupFilter)).Scan(&info.BooksCount)
	if err != nil {
		return nil, err
	}

	err = db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM authors`).Scan(&info.AuthorsCount)
	if err != nil {
		return nil, err
	}

	err = db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM catalogs`).Scan(&info.CatalogsCount)
	if err != nil {
		return nil, err
	}

	err = db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM genres`).Scan(&info.GenresCount)
	if err != nil {
		return nil, err
	}

	err = db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM series`).Scan(&info.SeriesCount)
	if err != nil {
		return nil, err
	}

	return &info, nil
}

// GetNewInfo gets statistics for new content
func (db *DB) GetNewInfo(ctx context.Context, period int) (*NewInfo, error) {
	var info NewInfo
	interval := fmt.Sprintf("%d days", period)

	err := db.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM books
		WHERE avail != 0 AND doublicat = 0 AND registerdate >= NOW() - $1::INTERVAL
	`, interval).Scan(&info.NewBooks)
	if err != nil {
		return nil, err
	}

	return &info, nil
}

// ZipIsScanned checks if a ZIP archive has been scanned
func (db *DB) ZipIsScanned(ctx context.Context, zipPath string) (bool, int64, error) {
	var catID int64
	err := db.pool.QueryRow(ctx, `
		SELECT cat_id FROM catalogs
		WHERE path = $1 AND cat_type = $2
	`, zipPath, CatZip).Scan(&catID)
	if err != nil {
		return false, 0, nil
	}
	return true, catID, nil
}

// GetAllZipCatalogs returns all ZIP catalog paths with their IDs
func (db *DB) GetAllZipCatalogs(ctx context.Context) (map[string]int64, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT cat_id, path FROM catalogs WHERE cat_type = $1
	`, CatZip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var catID int64
		var path string
		if err := rows.Scan(&catID, &path); err != nil {
			return nil, err
		}
		result[path] = catID
	}
	return result, rows.Err()
}

// MarkDuplicates marks duplicate books
func (db *DB) MarkDuplicates(ctx context.Context, mode DuplicateMode) error {
	if mode == DupClear {
		_, err := db.pool.Exec(ctx, "UPDATE books SET doublicat = 0")
		return err
	}

	if mode == DupNone {
		return nil
	}

	var query string
	if mode == DupNormal {
		// Group by title + authors (same title and same set of authors = duplicate)
		query = `
			WITH book_authors AS (
				SELECT b.book_id, b.title, b.filesize,
				       COALESCE(STRING_AGG(CAST(ba.author_id AS TEXT), ',' ORDER BY ba.author_id), '') as authors
				FROM books b
				LEFT JOIN bauthors ba ON b.book_id = ba.book_id
				WHERE b.avail != 0
				GROUP BY b.book_id, b.title, b.filesize
			),
			duplicates AS (
				SELECT book_id,
				       ROW_NUMBER() OVER (PARTITION BY title, authors ORDER BY filesize DESC, book_id) as rn,
				       FIRST_VALUE(book_id) OVER (PARTITION BY title, authors ORDER BY filesize DESC, book_id) as original_id
				FROM book_authors
			)
			UPDATE books
			SET doublicat = d.original_id
			FROM duplicates d
			WHERE books.book_id = d.book_id AND d.rn > 1
		`
	} else {
		// Group by title + format + filesize (exact same file = duplicate)
		query = `
			WITH duplicates AS (
				SELECT book_id,
				       ROW_NUMBER() OVER (PARTITION BY title, format, filesize ORDER BY book_id) as rn,
				       FIRST_VALUE(book_id) OVER (PARTITION BY title, format, filesize ORDER BY book_id) as original_id
				FROM books
				WHERE avail != 0
			)
			UPDATE books
			SET doublicat = d.original_id
			FROM duplicates d
			WHERE books.book_id = d.book_id AND d.rn > 1
		`
	}

	_, err := db.pool.Exec(ctx, query)
	return err
}

// GetDuplicateCount returns the number of duplicates for a book
func (db *DB) GetDuplicateCount(ctx context.Context, bookID int64) (int, error) {
	var count int
	err := db.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM books WHERE doublicat = $1 AND avail != 0
	`, bookID).Scan(&count)
	return count, err
}

// GetBookDuplicates returns all duplicates of a book (books that have this book as their original)
func (db *DB) GetBookDuplicates(ctx context.Context, bookID int64, page *Pagination) ([]Book, error) {
	// First check if this book is a duplicate itself - if so, find the original
	var originalID int64
	err := db.pool.QueryRow(ctx, `SELECT doublicat FROM books WHERE book_id = $1`, bookID).Scan(&originalID)
	if err != nil {
		return nil, err
	}

	// If this book is a duplicate, use its original; otherwise use the book itself
	targetID := bookID
	if originalID > 0 {
		targetID = originalID
	}

	// Get the original book and all its duplicates
	rows, err := db.pool.Query(ctx, `
		SELECT book_id, filename, path, format, filesize, title, annotation,
		       lang, docdate, cat_id, cat_type, avail, doublicat, cover, cover_type, favorite
		FROM books
		WHERE (book_id = $1 OR doublicat = $1) AND avail != 0
		ORDER BY filesize DESC
		LIMIT $2 OFFSET $3
	`, targetID, page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(
			&b.ID, &b.Filename, &b.Path, &b.Format, &b.Filesize, &b.Title,
			&b.Annotation, &b.Lang, &b.DocDate, &b.CatID, &b.CatType,
			&b.Avail, &b.Duplicate, &b.Cover, &b.CoverType, &b.Favorite,
		); err != nil {
			return nil, err
		}
		books = append(books, b)
	}

	// Get total count for pagination
	var count int64
	err = db.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM books WHERE (book_id = $1 OR doublicat = $1) AND avail != 0
	`, targetID).Scan(&count)
	if err == nil {
		page.TotalCount = count
		page.HasNext = int64(page.Offset()+page.Limit) < count
		page.HasPrev = page.Page > 0
	}

	return books, rows.Err()
}

// GetCatalog gets a catalog by ID
func (db *DB) GetCatalog(ctx context.Context, catID int64) (*Catalog, error) {
	var c Catalog
	err := db.pool.QueryRow(ctx, `
		SELECT cat_id, parent_id, cat_name, path, cat_type
		FROM catalogs
		WHERE cat_id = $1
	`, catID).Scan(&c.ID, &c.ParentID, &c.Name, &c.Path, &c.CatType)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// AddBookShelf adds a book to user's shelf
func (db *DB) AddBookShelf(ctx context.Context, user string, bookID int64) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO bookshelf (user_name, book_id, readtime)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, user, bookID, time.Now())
	return err
}

// RemoveBookShelf removes a book from user's bookshelf
func (db *DB) RemoveBookShelf(ctx context.Context, user string, bookID int64) error {
	_, err := db.pool.Exec(ctx, `
		DELETE FROM bookshelf WHERE user_name = $1 AND book_id = $2
	`, user, bookID)
	return err
}

// IsInBookShelf checks if a book is in user's bookshelf
func (db *DB) IsInBookShelf(ctx context.Context, user string, bookID int64) (bool, error) {
	var exists bool
	err := db.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM bookshelf WHERE user_name = $1 AND book_id = $2)
	`, user, bookID).Scan(&exists)
	return exists, err
}

// CountBookShelf returns the number of books in user's bookshelf
func (db *DB) CountBookShelf(ctx context.Context, user string) (int64, error) {
	var count int64
	err := db.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM bookshelf bs
		JOIN books b ON bs.book_id = b.book_id
		WHERE bs.user_name = $1 AND b.avail != 0
	`, user).Scan(&count)
	return count, err
}

// GetAuthorsByPrefix gets authors whose last name starts with prefix (case insensitive)
func (db *DB) GetAuthorsByPrefix(ctx context.Context, prefix string, page *Pagination) ([]Author, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT DISTINCT a.author_id, a.first_name, a.last_name
		FROM authors a
		JOIN bauthors ba ON a.author_id = ba.author_id
		JOIN books b ON ba.book_id = b.book_id
		WHERE b.avail != 0 AND UPPER(a.last_name) LIKE $1
		ORDER BY a.last_name, a.first_name
		LIMIT $2 OFFSET $3
	`, strings.ToUpper(prefix)+"%", page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var authors []Author
	for rows.Next() {
		var a Author
		if err := rows.Scan(&a.ID, &a.FirstName, &a.LastName); err != nil {
			return nil, err
		}
		authors = append(authors, a)
	}

	return authors, rows.Err()
}

// GetSeriesByPrefix gets series whose name starts with prefix (case insensitive)
func (db *DB) GetSeriesByPrefix(ctx context.Context, prefix string, page *Pagination) ([]Series, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT DISTINCT s.ser_id, s.ser
		FROM series s
		JOIN bseries bs ON s.ser_id = bs.ser_id
		JOIN books b ON bs.book_id = b.book_id
		WHERE b.avail != 0 AND UPPER(s.ser) LIKE $1
		ORDER BY s.ser
		LIMIT $2 OFFSET $3
	`, strings.ToUpper(prefix)+"%", page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var series []Series
	for rows.Next() {
		var s Series
		if err := rows.Scan(&s.ID, &s.Name); err != nil {
			return nil, err
		}
		series = append(series, s)
	}

	return series, rows.Err()
}

// GetBookShelf gets user's reading list
func (db *DB) GetBookShelf(ctx context.Context, user string, page *Pagination) ([]Book, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT b.book_id, b.filename, b.path, b.format, b.filesize, b.title, b.annotation,
		       b.lang, b.docdate, b.registerdate, b.cat_id, b.cat_type, b.avail, b.doublicat,
		       b.cover, b.cover_type, b.favorite
		FROM books b
		JOIN bookshelf bs ON b.book_id = bs.book_id
		WHERE bs.user_name = $1 AND b.avail != 0
		ORDER BY bs.readtime DESC
		LIMIT $2 OFFSET $3
	`, user, page.Limit, page.Offset())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(
			&b.ID, &b.Filename, &b.Path, &b.Format, &b.Filesize,
			&b.Title, &b.Annotation, &b.Lang, &b.DocDate,
			&b.RegisterDate, &b.CatID, &b.CatType, &b.Avail,
			&b.Duplicate, &b.Cover, &b.CoverType, &b.Favorite,
		); err != nil {
			return nil, err
		}
		books = append(books, b)
	}

	return books, rows.Err()
}
