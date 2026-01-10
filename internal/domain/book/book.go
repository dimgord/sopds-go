package book

import (
	"time"
)

// ID is a strongly typed identifier for books
type ID int64

// Book is the aggregate root for the library's core entity
type Book struct {
	id           ID
	filename     string
	path         string
	format       Format
	filesize     int64
	title        string
	annotation   string
	language     Language
	docDate      string
	registerDate time.Time
	catalogID    int64
	catalogType  CatalogType
	availability Availability
	duplicateOf  *ID
	cover        Cover
	favorite     bool
	// Audiobook fields
	isAudiobook     bool
	durationSeconds int
	bitrate         int
	trackCount      int
	chapters        string // JSON
}

// New creates a new Book with validation
func New(filename, path string, format Format, filesize int64) (*Book, error) {
	if filename == "" {
		return nil, ErrEmptyFilename
	}
	if filesize < 0 {
		return nil, ErrInvalidFilesize
	}

	return &Book{
		filename:     filename,
		path:         path,
		format:       format,
		filesize:     filesize,
		availability: AvailabilityVerified,
		registerDate: time.Now(),
		cover:        EmptyCover(),
	}, nil
}

// Reconstruct recreates a Book from persistence layer
// This bypasses validation for loading existing records
func Reconstruct(
	id ID,
	filename, path string,
	format Format,
	filesize int64,
	title, annotation, docDate string,
	lang Language,
	registerDate time.Time,
	catalogID int64,
	catalogType CatalogType,
	availability Availability,
	duplicateOf *ID,
	cover Cover,
	favorite bool,
	isAudiobook bool,
	durationSeconds, bitrate, trackCount int,
	chapters string,
) *Book {
	return &Book{
		id:              id,
		filename:        filename,
		path:            path,
		format:          format,
		filesize:        filesize,
		title:           title,
		annotation:      annotation,
		docDate:         docDate,
		language:        lang,
		registerDate:    registerDate,
		catalogID:       catalogID,
		catalogType:     catalogType,
		availability:    availability,
		duplicateOf:     duplicateOf,
		cover:           cover,
		favorite:        favorite,
		isAudiobook:     isAudiobook,
		durationSeconds: durationSeconds,
		bitrate:         bitrate,
		trackCount:      trackCount,
		chapters:        chapters,
	}
}

// --- Domain Methods ---

// MarkAsDuplicate marks this book as a duplicate of another
func (b *Book) MarkAsDuplicate(originalID ID) error {
	if originalID == b.id {
		return ErrSelfDuplicate
	}
	b.duplicateOf = &originalID
	return nil
}

// ClearDuplicateStatus removes the duplicate marker
func (b *Book) ClearDuplicateStatus() {
	b.duplicateOf = nil
}

// IsDuplicate returns true if this book is marked as a duplicate
func (b *Book) IsDuplicate() bool {
	return b.duplicateOf != nil
}

// MarkAsDeleted marks the book as logically deleted
func (b *Book) MarkAsDeleted() {
	b.availability = AvailabilityDeleted
}

// MarkAsPending marks the book for verification during scan
func (b *Book) MarkAsPending() {
	if b.availability == AvailabilityVerified {
		b.availability = AvailabilityPending
	}
}

// Verify confirms the book still exists on disk
func (b *Book) Verify() {
	if b.availability == AvailabilityPending {
		b.availability = AvailabilityVerified
	}
}

// IsAvailable returns true if the book is accessible
func (b *Book) IsAvailable() bool {
	return b.availability.IsAvailable()
}

// SetMetadata updates book metadata from parsing
func (b *Book) SetMetadata(title, annotation, docDate string, lang Language) {
	b.title = title
	b.annotation = annotation
	b.docDate = docDate
	b.language = lang
}

// SetCover updates the cover information
func (b *Book) SetCover(cover Cover) {
	b.cover = cover
}

// ToggleFavorite toggles the favorite status
func (b *Book) ToggleFavorite() {
	b.favorite = !b.favorite
}

// SetFavorite sets the favorite status
func (b *Book) SetFavorite(fav bool) {
	b.favorite = fav
}

// AssignToCatalog assigns the book to a catalog
func (b *Book) AssignToCatalog(catalogID int64, catalogType CatalogType) {
	b.catalogID = catalogID
	b.catalogType = catalogType
}

// SetID sets the book ID (used after persistence)
func (b *Book) SetID(id ID) {
	b.id = id
}

// --- Getters ---

func (b *Book) ID() ID                     { return b.id }
func (b *Book) Filename() string           { return b.filename }
func (b *Book) Path() string               { return b.path }
func (b *Book) Format() Format             { return b.format }
func (b *Book) Filesize() int64            { return b.filesize }
func (b *Book) Title() string              { return b.title }
func (b *Book) Annotation() string         { return b.annotation }
func (b *Book) Language() Language         { return b.language }
func (b *Book) DocDate() string            { return b.docDate }
func (b *Book) RegisterDate() time.Time    { return b.registerDate }
func (b *Book) CatalogID() int64           { return b.catalogID }
func (b *Book) CatalogType() CatalogType   { return b.catalogType }
func (b *Book) Availability() Availability { return b.availability }
func (b *Book) DuplicateOf() *ID           { return b.duplicateOf }
func (b *Book) Cover() Cover               { return b.cover }
func (b *Book) IsFavorite() bool           { return b.favorite }
func (b *Book) IsAudiobook() bool          { return b.isAudiobook }
func (b *Book) DurationSeconds() int       { return b.durationSeconds }
func (b *Book) Bitrate() int               { return b.bitrate }
func (b *Book) TrackCount() int            { return b.trackCount }
func (b *Book) Chapters() string           { return b.chapters }

// --- Reference types for relationships ---

// AuthorRef is a lightweight reference to an author
type AuthorRef struct {
	ID        int64
	FirstName string
	LastName  string
}

// FullName returns the author's display name
func (a AuthorRef) FullName() string {
	if a.FirstName == "" {
		return a.LastName
	}
	if a.LastName == "" {
		return a.FirstName
	}
	return a.LastName + " " + a.FirstName
}

// GenreRef is a lightweight reference to a genre
type GenreRef struct {
	ID         int64
	Code       string
	Section    string
	Subsection string
}

// DisplayName returns the genre's display name
func (g GenreRef) DisplayName() string {
	if g.Subsection != "" {
		return g.Subsection
	}
	return g.Code
}

// SeriesRef is a lightweight reference to a series
type SeriesRef struct {
	ID     int64
	Name   string
	Number int
}
