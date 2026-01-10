package catalog

import "strings"

// ID is a strongly typed identifier for catalogs
type ID int64

// Type represents the catalog storage type
type Type int

const (
	TypeNormal Type = 0 // Regular filesystem directory
	TypeZip    Type = 1 // ZIP archive
	TypeGzip   Type = 2 // GZIP compressed
)

// String returns a string representation
func (t Type) String() string {
	switch t {
	case TypeNormal:
		return "directory"
	case TypeZip:
		return "zip"
	case TypeGzip:
		return "gzip"
	default:
		return "unknown"
	}
}

// IsArchive returns true if this is an archive type
func (t Type) IsArchive() bool {
	return t == TypeZip || t == TypeGzip
}

// Catalog represents a directory or archive in the library
type Catalog struct {
	id       ID
	parentID *ID
	name     string
	path     string
	catType  Type
}

// New creates a new Catalog
func New(name, path string, catType Type, parentID *ID) *Catalog {
	return &Catalog{
		name:     name,
		path:     path,
		catType:  catType,
		parentID: parentID,
	}
}

// Reconstruct recreates a Catalog from persistence layer
func Reconstruct(id ID, parentID *ID, name, path string, catType Type) *Catalog {
	return &Catalog{
		id:       id,
		parentID: parentID,
		name:     name,
		path:     path,
		catType:  catType,
	}
}

// --- Domain Methods ---

// IsRoot returns true if this is a root catalog (no parent)
func (c *Catalog) IsRoot() bool {
	return c.parentID == nil
}

// IsArchive returns true if this catalog represents a ZIP/archive
func (c *Catalog) IsArchive() bool {
	return c.catType.IsArchive()
}

// IsZip returns true if this catalog is a ZIP archive
func (c *Catalog) IsZip() bool {
	return c.catType == TypeZip
}

// ContainsPath returns true if the given path is inside this catalog
func (c *Catalog) ContainsPath(path string) bool {
	return strings.HasPrefix(path, c.path)
}

// SetID sets the catalog ID (used after persistence)
func (c *Catalog) SetID(id ID) {
	c.id = id
}

// SetParentID sets the parent catalog ID
func (c *Catalog) SetParentID(parentID *ID) {
	c.parentID = parentID
}

// --- Getters ---

func (c *Catalog) ID() ID       { return c.id }
func (c *Catalog) ParentID() *ID { return c.parentID }
func (c *Catalog) Name() string   { return c.name }
func (c *Catalog) Path() string   { return c.path }
func (c *Catalog) Type() Type     { return c.catType }

// Item represents an item in a catalog listing (book or subcatalog)
type Item struct {
	ItemType   string // "catalog" or "book"
	ID         int64
	Name       string
	Path       string
	Title      string
	Annotation string
	DocDate    string
	Format     string
	Filesize   int64
	Cover      string
	CoverType  string
}

// IsBook returns true if this item is a book
func (i Item) IsBook() bool {
	return i.ItemType == "book"
}

// IsCatalog returns true if this item is a subcatalog
func (i Item) IsCatalog() bool {
	return i.ItemType == "catalog"
}
