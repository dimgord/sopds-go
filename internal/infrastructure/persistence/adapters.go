package persistence

import (
	"github.com/dimgord/sopds-go/internal/database"
	"github.com/dimgord/sopds-go/internal/domain/author"
	"github.com/dimgord/sopds-go/internal/domain/book"
	domainCatalog "github.com/dimgord/sopds-go/internal/domain/catalog"
	"github.com/dimgord/sopds-go/internal/domain/genre"
	"github.com/dimgord/sopds-go/internal/domain/repository"
	"github.com/dimgord/sopds-go/internal/domain/series"
)

// --- Book Adapters ---

// BookToLegacy converts a domain Book to legacy database.Book
func BookToLegacy(b *book.Book) *database.Book {
	if b == nil {
		return nil
	}

	// Convert *ID to *int64 (nil if nil)
	var duplicateOf *int64
	if b.DuplicateOf() != nil {
		id := int64(*b.DuplicateOf())
		duplicateOf = &id
	}

	return &database.Book{
		ID:              int64(b.ID()),
		Filename:        b.Filename(),
		Path:            b.Path(),
		Format:          b.Format().String(),
		Filesize:        b.Filesize(),
		Title:           b.Title(),
		Annotation:      b.Annotation(),
		Lang:            b.Language().String(),
		DocDate:         b.DocDate(),
		RegisterDate:    b.RegisterDate(),
		CatID:           b.CatalogID(),
		CatType:         database.CatType(b.CatalogType()),
		Avail:           database.Avail(b.Availability()),
		DuplicateOf:     duplicateOf,
		Cover:           b.Cover().Data(),
		CoverType:       b.Cover().ContentType(),
		Favorite:        b.IsFavorite(),
		IsAudiobook:     b.IsAudiobook(),
		DurationSeconds: b.DurationSeconds(),
		Bitrate:         b.Bitrate(),
		TrackCount:      b.TrackCount(),
		Chapters:        b.Chapters(),
	}
}

// BooksToLegacy converts domain Books to legacy database.Book slice
func BooksToLegacy(books []*book.Book) []database.Book {
	result := make([]database.Book, len(books))
	for i, b := range books {
		result[i] = *BookToLegacy(b)
	}
	return result
}

// --- Author Adapters ---

// AuthorToLegacy converts a domain Author to legacy database.Author
func AuthorToLegacy(a *author.Author) *database.Author {
	if a == nil {
		return nil
	}
	return &database.Author{
		ID:        int64(a.ID()),
		FirstName: a.FirstName(),
		LastName:  a.LastName(),
	}
}

// AuthorsToLegacy converts domain Authors to legacy database.Author slice
func AuthorsToLegacy(authors []*author.Author) []database.Author {
	result := make([]database.Author, len(authors))
	for i, a := range authors {
		result[i] = *AuthorToLegacy(a)
	}
	return result
}

// AuthorRefsToLegacy converts book.AuthorRef slice to legacy database.Author slice
func AuthorRefsToLegacy(refs []book.AuthorRef) []database.Author {
	result := make([]database.Author, len(refs))
	for i, ref := range refs {
		result[i] = database.Author{
			ID:        ref.ID,
			FirstName: ref.FirstName,
			LastName:  ref.LastName,
		}
	}
	return result
}

// --- Genre Adapters ---

// GenreToLegacy converts a domain Genre to legacy database.Genre
func GenreToLegacy(g *genre.Genre) *database.Genre {
	if g == nil {
		return nil
	}
	return &database.Genre{
		ID:         int64(g.ID()),
		Genre:      g.Code(),
		Section:    g.Section(),
		Subsection: g.Subsection(),
	}
}

// GenresToLegacy converts domain Genres to legacy database.Genre slice
func GenresToLegacy(genres []*genre.Genre) []database.Genre {
	result := make([]database.Genre, len(genres))
	for i, g := range genres {
		result[i] = *GenreToLegacy(g)
	}
	return result
}

// GenreRefsToLegacy converts book.GenreRef slice to legacy database.Genre slice
func GenreRefsToLegacy(refs []book.GenreRef) []database.Genre {
	result := make([]database.Genre, len(refs))
	for i, ref := range refs {
		result[i] = database.Genre{
			ID:         ref.ID,
			Genre:      ref.Code,
			Section:    ref.Section,
			Subsection: ref.Subsection,
		}
	}
	return result
}

// --- Series Adapters ---

// SeriesToLegacy converts a domain Series to legacy database.Series
func SeriesToLegacy(s *series.Series) *database.Series {
	if s == nil {
		return nil
	}
	return &database.Series{
		ID:   int64(s.ID()),
		Name: s.Name(),
	}
}

// SeriesSliceToLegacy converts domain Series to legacy database.Series slice
func SeriesSliceToLegacy(seriesList []*series.Series) []database.Series {
	result := make([]database.Series, len(seriesList))
	for i, s := range seriesList {
		result[i] = *SeriesToLegacy(s)
	}
	return result
}

// SeriesRefsToLegacy converts book.SeriesRef slice to legacy database.BookSeries slice
func SeriesRefsToLegacy(refs []book.SeriesRef) []database.BookSeries {
	result := make([]database.BookSeries, len(refs))
	for i, ref := range refs {
		result[i] = database.BookSeries{
			SeriesID: ref.ID,
			Name:     ref.Name,
			SerNo:    ref.Number,
		}
	}
	return result
}

// --- Catalog Adapters ---

// CatalogToLegacy converts a domain Catalog to legacy database.Catalog
func CatalogToLegacy(c *domainCatalog.Catalog) *database.Catalog {
	if c == nil {
		return nil
	}
	var parentID *int64
	if c.ParentID() != nil {
		id := int64(*c.ParentID())
		parentID = &id
	}
	return &database.Catalog{
		ID:       int64(c.ID()),
		ParentID: parentID,
		Name:     c.Name(),
		Path:     c.Path(),
		CatType:  database.CatType(c.Type()),
	}
}

// CatalogsToLegacy converts domain Catalogs to legacy database.Catalog slice
func CatalogsToLegacy(catalogs []*domainCatalog.Catalog) []database.Catalog {
	result := make([]database.Catalog, len(catalogs))
	for i, c := range catalogs {
		result[i] = *CatalogToLegacy(c)
	}
	return result
}

// CatalogItemToLegacy converts domain catalog.Item to legacy database.CatalogItem
func CatalogItemToLegacy(item domainCatalog.Item) database.CatalogItem {
	return database.CatalogItem{
		ItemType:   item.ItemType,
		ID:         item.ID,
		Name:       item.Name,
		Path:       item.Path,
		Title:      item.Title,
		Annotation: item.Annotation,
		DocDate:    item.DocDate,
		Format:     item.Format,
		Filesize:   item.Filesize,
		Cover:      item.Cover,
		CoverType:  item.CoverType,
	}
}

// CatalogItemsToLegacy converts domain catalog.Item slice to legacy database.CatalogItem slice
func CatalogItemsToLegacy(items []domainCatalog.Item) []database.CatalogItem {
	result := make([]database.CatalogItem, len(items))
	for i, item := range items {
		result[i] = CatalogItemToLegacy(item)
	}
	return result
}

// --- Pagination Adapters ---

// PaginationToLegacy converts repository.Pagination to legacy database.Pagination
func PaginationToLegacy(p *repository.Pagination) *database.Pagination {
	if p == nil {
		return nil
	}
	return &database.Pagination{
		Page:       p.Page,
		Limit:      p.PageSize,
		TotalCount: p.TotalCount,
		HasNext:    p.HasNext,
		HasPrev:    p.HasPrev,
	}
}

// PaginationFromLegacy converts legacy database.Pagination to repository.Pagination
func PaginationFromLegacy(p *database.Pagination) *repository.Pagination {
	if p == nil {
		return nil
	}
	return &repository.Pagination{
		Page:       p.Page,
		PageSize:   p.Limit,
		TotalCount: p.TotalCount,
		HasNext:    p.HasNext,
		HasPrev:    p.HasPrev,
	}
}

// --- Statistics Adapters ---

// LanguageStatsToLegacy converts repository.LanguageStats to legacy database.LanguageInfo
func LanguageStatsToLegacy(stats []repository.LanguageStats) []database.LanguageInfo {
	result := make([]database.LanguageInfo, len(stats))
	for i, s := range stats {
		result[i] = database.LanguageInfo{
			Code:  s.Code,
			Count: s.Count,
		}
	}
	return result
}

// --- Helper functions ---

func boolToIntAdapter(b bool) int {
	if b {
		return 1
	}
	return 0
}
