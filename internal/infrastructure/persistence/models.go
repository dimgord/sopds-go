package persistence

import (
	"time"
)

// BookModel is the GORM model for books table
type BookModel struct {
	ID           int64      `gorm:"column:book_id;primaryKey;autoIncrement"`
	Filename     string     `gorm:"column:filename;size:256;not null"`
	Path         string     `gorm:"column:path;size:1024;not null"`
	Filesize     int64      `gorm:"column:filesize;default:0"`
	Format       string     `gorm:"column:format;size:8"`
	CatID        *int64     `gorm:"column:cat_id;index:idx_books_cat_id"`
	CatType      int        `gorm:"column:cat_type;default:0"`
	RegisterDate time.Time  `gorm:"column:registerdate;autoCreateTime;index:idx_books_registerdate"`
	DocDate      string     `gorm:"column:docdate;size:32"`
	Favorite     bool       `gorm:"column:favorite;default:false"`
	Lang         string     `gorm:"column:lang;size:16;index:idx_books_lang"`
	Title        string     `gorm:"column:title;size:256"`
	Annotation   string     `gorm:"column:annotation;type:text"`
	Cover        string     `gorm:"column:cover;size:32"`
	CoverType    string     `gorm:"column:cover_type;size:32"`
	DuplicateOf  *int64     `gorm:"column:duplicate_of;index:idx_books_avail_duplicate"`
	Avail        int        `gorm:"column:avail;default:0;index:idx_books_avail_duplicate"`
	SearchVector string     `gorm:"column:search_vector;type:tsvector;->"` // Read-only, managed by trigger
	// Audiobook fields
	DurationSeconds int     `gorm:"column:duration_seconds;default:0"`
	Bitrate         int     `gorm:"column:bitrate;default:0"`
	IsAudiobook     bool    `gorm:"column:is_audiobook;default:false;index:idx_books_is_audiobook"`
	TrackCount      int     `gorm:"column:track_count;default:0"`
	Chapters        *string `gorm:"column:chapters;type:jsonb"` // NULL when empty, valid JSON when set

	// Relationships (loaded via Preload)
	Authors       []AuthorModel `gorm:"many2many:bauthors;joinForeignKey:book_id;joinReferences:author_id"`
	Genres        []GenreModel  `gorm:"many2many:bgenres;joinForeignKey:book_id;joinReferences:genre_id"`
	Series        []SeriesModel `gorm:"many2many:bseries;joinForeignKey:book_id;joinReferences:ser_id"`
	Catalog       *CatalogModel `gorm:"foreignKey:CatID;references:ID"`
	OriginalBook  *BookModel    `gorm:"foreignKey:DuplicateOf;references:ID"` // The original book this is a duplicate of
}

// TableName returns the table name for BookModel
func (BookModel) TableName() string {
	return "books"
}

// AuthorModel is the GORM model for authors table
type AuthorModel struct {
	ID        int64  `gorm:"column:author_id;primaryKey;autoIncrement"`
	FirstName string `gorm:"column:first_name;size:64;not null;default:''"`
	LastName  string `gorm:"column:last_name;size:64;not null;default:'';index:idx_authors_name"`

	// Relationships
	Books []BookModel `gorm:"many2many:bauthors;joinForeignKey:author_id;joinReferences:book_id"`
}

// TableName returns the table name for AuthorModel
func (AuthorModel) TableName() string {
	return "authors"
}

// GenreModel is the GORM model for genres table
type GenreModel struct {
	ID         int64  `gorm:"column:genre_id;primaryKey;autoIncrement"`
	Genre      string `gorm:"column:genre;size:32;not null;uniqueIndex"`
	Section    string `gorm:"column:section;size:64;not null;default:''"`
	Subsection string `gorm:"column:subsection;size:100;not null;default:''"`

	// Relationships
	Books []BookModel `gorm:"many2many:bgenres;joinForeignKey:genre_id;joinReferences:book_id"`
}

// TableName returns the table name for GenreModel
func (GenreModel) TableName() string {
	return "genres"
}

// SeriesModel is the GORM model for series table
type SeriesModel struct {
	ID   int64  `gorm:"column:ser_id;primaryKey;autoIncrement"`
	Name string `gorm:"column:ser;size:128;not null;uniqueIndex"`

	// Relationships
	Books []BookModel `gorm:"many2many:bseries;joinForeignKey:ser_id;joinReferences:book_id"`
}

// TableName returns the table name for SeriesModel
func (SeriesModel) TableName() string {
	return "series"
}

// BookAuthorModel is the GORM model for bauthors junction table
type BookAuthorModel struct {
	BookID   int64  `gorm:"column:book_id;primaryKey"`
	AuthorID int64  `gorm:"column:author_id;primaryKey;index:idx_bauthors_author"`
	Role     string `gorm:"column:role;size:16;default:'author';index:idx_bauthors_role"` // 'author' or 'narrator'
}

// TableName returns the table name for BookAuthorModel
func (BookAuthorModel) TableName() string {
	return "bauthors"
}

// AuthorRole constants
const (
	RoleAuthor   = "author"
	RoleNarrator = "narrator"
)

// BookGenreModel is the GORM model for bgenres junction table
type BookGenreModel struct {
	BookID  int64 `gorm:"column:book_id;primaryKey"`
	GenreID int64 `gorm:"column:genre_id;primaryKey;index:idx_bgenres_genre"`
}

// TableName returns the table name for BookGenreModel
func (BookGenreModel) TableName() string {
	return "bgenres"
}

// BookSeriesModel is the GORM model for bseries junction table (with extra column)
type BookSeriesModel struct {
	BookID   int64 `gorm:"column:book_id;primaryKey"`
	SeriesID int64 `gorm:"column:ser_id;primaryKey;index:idx_bseries_ser"`
	SerNo    int   `gorm:"column:ser_no;default:0"`
}

// TableName returns the table name for BookSeriesModel
func (BookSeriesModel) TableName() string {
	return "bseries"
}

// CatalogModel is the GORM model for catalogs table
type CatalogModel struct {
	ID       int64  `gorm:"column:cat_id;primaryKey;autoIncrement"`
	ParentID *int64 `gorm:"column:parent_id;index:idx_catalogs_parent"`
	Name     string `gorm:"column:cat_name;size:64;not null"`
	Path     string `gorm:"column:path;size:1024;not null"`
	CatType  int    `gorm:"column:cat_type;default:0"`

	// Relationships
	Parent   *CatalogModel  `gorm:"foreignKey:ParentID;references:ID"`
	Children []CatalogModel `gorm:"foreignKey:ParentID;references:ID"`
	Books    []BookModel    `gorm:"foreignKey:CatID;references:ID"`
}

// TableName returns the table name for CatalogModel
func (CatalogModel) TableName() string {
	return "catalogs"
}

// BookshelfModel is the GORM model for bookshelf table
type BookshelfModel struct {
	UserName string    `gorm:"column:user_name;size:32;primaryKey"`
	BookID   int64     `gorm:"column:book_id;primaryKey"`
	ReadTime time.Time `gorm:"column:readtime;autoCreateTime"`

	// Relationships
	Book *BookModel `gorm:"foreignKey:BookID;references:ID"`
}

// TableName returns the table name for BookshelfModel
func (BookshelfModel) TableName() string {
	return "bookshelf"
}

// CatalogItemModel is a virtual model for catalog content queries (UNION result)
type CatalogItemModel struct {
	ItemType   string    `gorm:"column:item_type"`
	ID         int64     `gorm:"column:id"`
	Name       string    `gorm:"column:name"`
	Path       string    `gorm:"column:path"`
	Date       time.Time `gorm:"column:date"`
	Title      string    `gorm:"column:title"`
	Annotation string    `gorm:"column:annotation"`
	DocDate    string    `gorm:"column:docdate"`
	Format     string    `gorm:"column:format"`
	Filesize   int64     `gorm:"column:filesize"`
	Cover      string    `gorm:"column:cover"`
	CoverType  string    `gorm:"column:cover_type"`
}
