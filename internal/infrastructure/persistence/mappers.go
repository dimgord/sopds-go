package persistence

import (
	"github.com/dimgord/sopds-go/internal/domain/author"
	"github.com/dimgord/sopds-go/internal/domain/book"
	"github.com/dimgord/sopds-go/internal/domain/catalog"
	"github.com/dimgord/sopds-go/internal/domain/genre"
	"github.com/dimgord/sopds-go/internal/domain/series"
)

// --- Book Mappers ---

// BookToModel converts a domain Book to a persistence model
func BookToModel(b *book.Book) *BookModel {
	var catID *int64
	if b.CatalogID() != 0 {
		id := b.CatalogID()
		catID = &id
	}

	var duplicateOf *int64
	if b.DuplicateOf() != nil {
		id := int64(*b.DuplicateOf())
		duplicateOf = &id
	}

	// Handle chapters - only set pointer if non-empty
	var chapters *string
	if b.Chapters() != "" {
		ch := b.Chapters()
		chapters = &ch
	}

	return &BookModel{
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
		CatID:           catID,
		CatType:         int(b.CatalogType()),
		Avail:           int(b.Availability()),
		DuplicateOf:     duplicateOf,
		Cover:           b.Cover().Data(),
		CoverType:       b.Cover().ContentType(),
		Favorite:        b.IsFavorite(),
		IsAudiobook:     b.IsAudiobook(),
		DurationSeconds: b.DurationSeconds(),
		Bitrate:         b.Bitrate(),
		TrackCount:      b.TrackCount(),
		Chapters:        chapters,
	}
}

// BookToDomain converts a persistence model to a domain Book
func BookToDomain(m *BookModel) *book.Book {
	var catalogID int64
	if m.CatID != nil {
		catalogID = *m.CatID
	}

	var duplicateOf *book.ID
	if m.DuplicateOf != nil {
		id := book.ID(*m.DuplicateOf)
		duplicateOf = &id
	}

	// Handle chapters pointer
	var chapters string
	if m.Chapters != nil {
		chapters = *m.Chapters
	}

	return book.Reconstruct(
		book.ID(m.ID),
		m.Filename,
		m.Path,
		book.ParseFormat(m.Format),
		m.Filesize,
		m.Title,
		m.Annotation,
		m.DocDate,
		book.Language(m.Lang),
		m.RegisterDate,
		catalogID,
		book.CatalogType(m.CatType),
		book.Availability(m.Avail),
		duplicateOf,
		book.NewCover(m.Cover, m.CoverType),
		m.Favorite,
		m.IsAudiobook,
		m.DurationSeconds,
		m.Bitrate,
		m.TrackCount,
		chapters,
	)
}

// BooksToDomain converts a slice of persistence models to domain Books
func BooksToDomain(models []*BookModel) []*book.Book {
	books := make([]*book.Book, len(models))
	for i, m := range models {
		books[i] = BookToDomain(m)
	}
	return books
}

// BooksModelsToDomain converts a slice of value models to domain Books
func BooksModelsToDomain(models []BookModel) []*book.Book {
	books := make([]*book.Book, len(models))
	for i := range models {
		books[i] = BookToDomain(&models[i])
	}
	return books
}

// --- Author Mappers ---

// AuthorToModel converts a domain Author to a persistence model
func AuthorToModel(a *author.Author) *AuthorModel {
	return &AuthorModel{
		ID:        int64(a.ID()),
		FirstName: a.FirstName(),
		LastName:  a.LastName(),
	}
}

// AuthorToDomain converts a persistence model to a domain Author
func AuthorToDomain(m *AuthorModel) *author.Author {
	return author.Reconstruct(
		author.ID(m.ID),
		m.FirstName,
		m.LastName,
	)
}

// AuthorsToDomain converts a slice of persistence models to domain Authors
func AuthorsToDomain(models []*AuthorModel) []*author.Author {
	authors := make([]*author.Author, len(models))
	for i, m := range models {
		authors[i] = AuthorToDomain(m)
	}
	return authors
}

// AuthorsModelsToDomain converts a slice of value models to domain Authors
func AuthorsModelsToDomain(models []AuthorModel) []*author.Author {
	authors := make([]*author.Author, len(models))
	for i := range models {
		authors[i] = AuthorToDomain(&models[i])
	}
	return authors
}

// AuthorToRef converts a persistence model to an AuthorRef
func AuthorToRef(m *AuthorModel) book.AuthorRef {
	return book.AuthorRef{
		ID:        m.ID,
		FirstName: m.FirstName,
		LastName:  m.LastName,
	}
}

// AuthorsToRefs converts a slice of persistence models to AuthorRefs
func AuthorsToRefs(models []AuthorModel) []book.AuthorRef {
	refs := make([]book.AuthorRef, len(models))
	for i := range models {
		refs[i] = AuthorToRef(&models[i])
	}
	return refs
}

// --- Genre Mappers ---

// GenreToModel converts a domain Genre to a persistence model
func GenreToModel(g *genre.Genre) *GenreModel {
	return &GenreModel{
		ID:         int64(g.ID()),
		Genre:      g.Code(),
		Section:    g.Section(),
		Subsection: g.Subsection(),
	}
}

// GenreToDomain converts a persistence model to a domain Genre
func GenreToDomain(m *GenreModel) *genre.Genre {
	return genre.Reconstruct(
		genre.ID(m.ID),
		m.Genre,
		m.Section,
		m.Subsection,
	)
}

// GenresToDomain converts a slice of persistence models to domain Genres
func GenresToDomain(models []*GenreModel) []*genre.Genre {
	genres := make([]*genre.Genre, len(models))
	for i, m := range models {
		genres[i] = GenreToDomain(m)
	}
	return genres
}

// GenresModelsToDomain converts a slice of value models to domain Genres
func GenresModelsToDomain(models []GenreModel) []*genre.Genre {
	genres := make([]*genre.Genre, len(models))
	for i := range models {
		genres[i] = GenreToDomain(&models[i])
	}
	return genres
}

// GenreToRef converts a persistence model to a GenreRef
func GenreToRef(m *GenreModel) book.GenreRef {
	return book.GenreRef{
		ID:         m.ID,
		Code:       m.Genre,
		Section:    m.Section,
		Subsection: m.Subsection,
	}
}

// GenresToRefs converts a slice of persistence models to GenreRefs
func GenresToRefs(models []GenreModel) []book.GenreRef {
	refs := make([]book.GenreRef, len(models))
	for i := range models {
		refs[i] = GenreToRef(&models[i])
	}
	return refs
}

// --- Series Mappers ---

// SeriesToModel converts a domain Series to a persistence model
func SeriesToModel(s *series.Series) *SeriesModel {
	return &SeriesModel{
		ID:   int64(s.ID()),
		Name: s.Name(),
	}
}

// SeriesToDomain converts a persistence model to a domain Series
func SeriesToDomain(m *SeriesModel) *series.Series {
	return series.Reconstruct(
		series.ID(m.ID),
		m.Name,
	)
}

// SeriesSliceToDomain converts a slice of persistence models to domain Series
func SeriesSliceToDomain(models []*SeriesModel) []*series.Series {
	result := make([]*series.Series, len(models))
	for i, m := range models {
		result[i] = SeriesToDomain(m)
	}
	return result
}

// SeriesModelsToDomain converts a slice of value models to domain Series
func SeriesModelsToDomain(models []SeriesModel) []*series.Series {
	result := make([]*series.Series, len(models))
	for i := range models {
		result[i] = SeriesToDomain(&models[i])
	}
	return result
}

// SeriesToRef converts a persistence model to a SeriesRef (without number)
func SeriesToRef(m *SeriesModel) book.SeriesRef {
	return book.SeriesRef{
		ID:   m.ID,
		Name: m.Name,
	}
}

// BookSeriesToRef converts a BookSeriesModel to a SeriesRef with number
func BookSeriesToRef(bs *BookSeriesModel, name string) book.SeriesRef {
	return book.SeriesRef{
		ID:     bs.SeriesID,
		Name:   name,
		Number: bs.SerNo,
	}
}

// --- Catalog Mappers ---

// CatalogToModel converts a domain Catalog to a persistence model
func CatalogToModel(c *catalog.Catalog) *CatalogModel {
	var parentID *int64
	if c.ParentID() != nil {
		id := int64(*c.ParentID())
		parentID = &id
	}

	return &CatalogModel{
		ID:       int64(c.ID()),
		ParentID: parentID,
		Name:     c.Name(),
		Path:     c.Path(),
		CatType:  int(c.Type()),
	}
}

// CatalogToDomain converts a persistence model to a domain Catalog
func CatalogToDomain(m *CatalogModel) *catalog.Catalog {
	var parentID *catalog.ID
	if m.ParentID != nil {
		id := catalog.ID(*m.ParentID)
		parentID = &id
	}

	return catalog.Reconstruct(
		catalog.ID(m.ID),
		parentID,
		m.Name,
		m.Path,
		catalog.Type(m.CatType),
	)
}

// CatalogsToDomain converts a slice of persistence models to domain Catalogs
func CatalogsToDomain(models []*CatalogModel) []*catalog.Catalog {
	catalogs := make([]*catalog.Catalog, len(models))
	for i, m := range models {
		catalogs[i] = CatalogToDomain(m)
	}
	return catalogs
}

// CatalogsModelsToDomain converts a slice of value models to domain Catalogs
func CatalogsModelsToDomain(models []CatalogModel) []*catalog.Catalog {
	catalogs := make([]*catalog.Catalog, len(models))
	for i := range models {
		catalogs[i] = CatalogToDomain(&models[i])
	}
	return catalogs
}

// CatalogItemToDomain converts a CatalogItemModel to a domain Item
func CatalogItemToDomain(m *CatalogItemModel) catalog.Item {
	return catalog.Item{
		ItemType:   m.ItemType,
		ID:         m.ID,
		Name:       m.Name,
		Path:       m.Path,
		Title:      m.Title,
		Annotation: m.Annotation,
		DocDate:    m.DocDate,
		Format:     m.Format,
		Filesize:   m.Filesize,
		Cover:      m.Cover,
		CoverType:  m.CoverType,
	}
}

// CatalogItemsToDomain converts a slice of CatalogItemModels to domain Items
func CatalogItemsToDomain(models []CatalogItemModel) []catalog.Item {
	items := make([]catalog.Item, len(models))
	for i := range models {
		items[i] = CatalogItemToDomain(&models[i])
	}
	return items
}

