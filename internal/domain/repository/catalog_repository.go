package repository

import (
	"context"

	"github.com/sopds/sopds-go/internal/domain/catalog"
)

// CatalogRepository defines the interface for catalog persistence
type CatalogRepository interface {
	// --- CRUD Operations ---

	// FindByID retrieves a catalog by ID
	FindByID(ctx context.Context, id catalog.ID) (*catalog.Catalog, error)

	// FindByNameAndPath finds a catalog by name and path (unique constraint)
	FindByNameAndPath(ctx context.Context, name, path string) (*catalog.Catalog, error)

	// FindByPath finds a catalog by its full path
	FindByPath(ctx context.Context, path string) (*catalog.Catalog, error)

	// Save persists a catalog (insert or update)
	Save(ctx context.Context, c *catalog.Catalog) error

	// Delete removes a catalog by ID
	Delete(ctx context.Context, id catalog.ID) error

	// --- Query Operations ---

	// Find returns catalogs matching the query (filters + sort + pagination)
	Find(ctx context.Context, query *CatalogQuery) ([]*catalog.Catalog, error)

	// Count returns the total count of catalogs matching filters
	Count(ctx context.Context, filters *CatalogFilters) (int64, error)

	// --- Tree Operations ---

	// GetOrCreateTree creates catalog tree from path parts, returns leaf ID
	GetOrCreateTree(ctx context.Context, pathParts []string, catType catalog.Type) (catalog.ID, error)

	// GetChildren returns child catalogs of a parent
	GetChildren(ctx context.Context, parentID catalog.ID, sort Sort) ([]*catalog.Catalog, error)

	// GetRoots returns root catalogs (no parent)
	GetRoots(ctx context.Context, sort Sort) ([]*catalog.Catalog, error)

	// --- Catalog Contents ---

	// GetItems returns books and subcatalogs in a catalog
	// Applies book filters and pagination
	GetItems(ctx context.Context, catalogID catalog.ID, bookFilters *BookFilters, sort Sort, pagination *Pagination) ([]catalog.Item, error)

	// CountItems returns the total count of items in a catalog
	CountItems(ctx context.Context, catalogID catalog.ID, bookFilters *BookFilters) (int64, error)

	// --- Archive Management ---

	// FindZipByPath finds a ZIP catalog by its path
	FindZipByPath(ctx context.Context, path string) (*catalog.Catalog, error)

	// GetAllZipCatalogs returns all ZIP catalogs as path->ID map
	GetAllZipCatalogs(ctx context.Context) (map[string]catalog.ID, error)

	// DeleteWithBooks deletes catalogs and marks their books as deleted
	// Returns the number of books affected
	DeleteWithBooks(ctx context.Context, catalogIDs []catalog.ID) (int64, error)
}
