package persistence

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dimgord/sopds-go/internal/domain/catalog"
	"github.com/dimgord/sopds-go/internal/domain/repository"
)

// CatalogRepository implements repository.CatalogRepository using GORM
type CatalogRepository struct {
	db *gorm.DB
}

// NewCatalogRepository creates a new CatalogRepository
func NewCatalogRepository(db *DB) *CatalogRepository {
	return &CatalogRepository{db: db.DB}
}

// --- CRUD Operations ---

// FindByID retrieves a catalog by ID
func (r *CatalogRepository) FindByID(ctx context.Context, id catalog.ID) (*catalog.Catalog, error) {
	var model CatalogModel
	err := r.db.WithContext(ctx).
		Where("cat_id = ?", int64(id)).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find catalog by id: %w", err)
	}

	return CatalogToDomain(&model), nil
}

// FindByNameAndPath finds a catalog by name and path
func (r *CatalogRepository) FindByNameAndPath(ctx context.Context, name, path string) (*catalog.Catalog, error) {
	var model CatalogModel
	err := r.db.WithContext(ctx).
		Where("cat_name = ? AND path = ?", name, path).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find catalog by name and path: %w", err)
	}

	return CatalogToDomain(&model), nil
}

// FindByPath finds a catalog by its full path
func (r *CatalogRepository) FindByPath(ctx context.Context, path string) (*catalog.Catalog, error) {
	var model CatalogModel
	err := r.db.WithContext(ctx).
		Where("path = ?", path).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find catalog by path: %w", err)
	}

	return CatalogToDomain(&model), nil
}

// Save persists a catalog (insert or update)
func (r *CatalogRepository) Save(ctx context.Context, c *catalog.Catalog) error {
	model := CatalogToModel(c)

	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "path"}},
			UpdateAll: true,
		}).
		Create(model).Error

	if err != nil {
		return fmt.Errorf("save catalog: %w", err)
	}

	// Update the domain object with the new ID
	c.SetID(catalog.ID(model.ID))

	return nil
}

// Delete removes a catalog by ID
func (r *CatalogRepository) Delete(ctx context.Context, id catalog.ID) error {
	err := r.db.WithContext(ctx).
		Delete(&CatalogModel{}, "cat_id = ?", int64(id)).Error
	if err != nil {
		return fmt.Errorf("delete catalog: %w", err)
	}
	return nil
}

// --- Query Operations ---

// Find returns catalogs matching the query
func (r *CatalogRepository) Find(ctx context.Context, query *repository.CatalogQuery) ([]*catalog.Catalog, error) {
	var models []CatalogModel

	db := r.db.WithContext(ctx).Model(&CatalogModel{})

	if query != nil {
		db = db.Scopes(ApplyCatalogFilters(query.Filters))
		db = db.Scopes(ApplyNameSort(query.Sort, "cat_name"))
		db = db.Scopes(Paginate(query.Pagination))
	} else {
		db = db.Order("cat_name ASC")
	}

	if err := db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("find catalogs: %w", err)
	}

	return CatalogsModelsToDomain(models), nil
}

// Count returns the total count of catalogs matching filters
func (r *CatalogRepository) Count(ctx context.Context, filters *repository.CatalogFilters) (int64, error) {
	var count int64

	db := r.db.WithContext(ctx).Model(&CatalogModel{})
	db = db.Scopes(ApplyCatalogFilters(filters))

	if err := db.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count catalogs: %w", err)
	}

	return count, nil
}

// --- Tree Operations ---

// GetOrCreateTree creates catalog tree from path parts, returns leaf ID
func (r *CatalogRepository) GetOrCreateTree(ctx context.Context, pathParts []string, catType catalog.Type) (catalog.ID, error) {
	if len(pathParts) == 0 {
		return 0, nil
	}

	var parentID *int64
	var currentPath string
	var leafID catalog.ID

	for _, part := range pathParts {
		if currentPath == "" {
			currentPath = part
		} else {
			currentPath = currentPath + "/" + part
		}

		// Try to find existing catalog
		var model CatalogModel
		err := r.db.WithContext(ctx).
			Where("path = ?", currentPath).
			First(&model).Error

		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Create new catalog
			model = CatalogModel{
				ParentID: parentID,
				Name:     part,
				Path:     currentPath,
				CatType:  int(catType),
			}

			err = r.db.WithContext(ctx).Create(&model).Error
			if err != nil {
				// Check for unique constraint violation (race condition)
				err = r.db.WithContext(ctx).Where("path = ?", currentPath).First(&model).Error
				if err != nil {
					return 0, fmt.Errorf("create catalog tree: %w", err)
				}
			}
		} else if err != nil {
			return 0, fmt.Errorf("find catalog in tree: %w", err)
		}

		parentID = &model.ID
		leafID = catalog.ID(model.ID)
	}

	return leafID, nil
}

// GetChildren returns child catalogs of a parent
func (r *CatalogRepository) GetChildren(ctx context.Context, parentID catalog.ID, sort repository.Sort) ([]*catalog.Catalog, error) {
	var models []CatalogModel

	db := r.db.WithContext(ctx).
		Model(&CatalogModel{}).
		Where("parent_id = ?", int64(parentID))

	db = db.Scopes(ApplyNameSort(sort, "cat_name"))

	if err := db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("get children: %w", err)
	}

	return CatalogsModelsToDomain(models), nil
}

// GetRoots returns root catalogs (no parent)
func (r *CatalogRepository) GetRoots(ctx context.Context, sort repository.Sort) ([]*catalog.Catalog, error) {
	var models []CatalogModel

	db := r.db.WithContext(ctx).
		Model(&CatalogModel{}).
		Where("parent_id IS NULL")

	db = db.Scopes(ApplyNameSort(sort, "cat_name"))

	if err := db.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("get roots: %w", err)
	}

	return CatalogsModelsToDomain(models), nil
}

// --- Catalog Contents ---

// GetItems returns books and subcatalogs in a catalog using UNION query
func (r *CatalogRepository) GetItems(ctx context.Context, catalogID catalog.ID, bookFilters *repository.BookFilters, sort repository.Sort, pagination *repository.Pagination) ([]catalog.Item, error) {
	// Build UNION query for catalogs and books
	dir := "ASC"
	if sort.IsDescending() {
		dir = "DESC"
	}

	var orderCol string
	switch sort.Field {
	case repository.SortByTitle:
		orderCol = "name"
	case repository.SortByDate:
		orderCol = "date"
	case repository.SortBySize:
		orderCol = "filesize"
	default:
		orderCol = "name"
	}

	// Build availability conditions for books
	availCond := "b.avail != 0"
	if bookFilters != nil && bookFilters.ShowDeleted {
		availCond = "1=1"
	}
	dupCond := "b.duplicate_of IS NULL"
	if bookFilters != nil && bookFilters.ShowDuplicates {
		dupCond = "1=1"
	}

	// Raw SQL with UNION
	sql := fmt.Sprintf(`
		SELECT 'catalog' as item_type, cat_id as id, cat_name as name, path,
			   NOW() as date, '' as title, '' as annotation, '' as docdate,
			   '' as format, 0 as filesize, '' as cover, '' as cover_type
		FROM catalogs
		WHERE parent_id = ?
		UNION ALL
		SELECT 'book' as item_type, b.book_id as id, b.filename as name, b.path,
			   b.registerdate as date, b.title, b.annotation, b.docdate,
			   b.format, b.filesize, b.cover, b.cover_type
		FROM books b
		WHERE b.cat_id = ? AND %s AND %s
		ORDER BY %s %s
	`, availCond, dupCond, orderCol, dir)

	// Apply pagination
	if pagination != nil && pagination.PageSize > 0 {
		sql += fmt.Sprintf(" LIMIT %d OFFSET %d", pagination.Limit(), pagination.Offset())
	}

	var items []CatalogItemModel
	if err := r.db.WithContext(ctx).Raw(sql, int64(catalogID), int64(catalogID)).Scan(&items).Error; err != nil {
		return nil, fmt.Errorf("get items: %w", err)
	}

	return CatalogItemsToDomain(items), nil
}

// CountItems returns the total count of items in a catalog
func (r *CatalogRepository) CountItems(ctx context.Context, catalogID catalog.ID, bookFilters *repository.BookFilters) (int64, error) {
	// Build availability conditions
	availCond := "b.avail != 0"
	if bookFilters != nil && bookFilters.ShowDeleted {
		availCond = "1=1"
	}
	dupCond := "b.duplicate_of IS NULL"
	if bookFilters != nil && bookFilters.ShowDuplicates {
		dupCond = "1=1"
	}

	sql := fmt.Sprintf(`
		SELECT
			(SELECT COUNT(*) FROM catalogs WHERE parent_id = ?) +
			(SELECT COUNT(*) FROM books b WHERE b.cat_id = ? AND %s AND %s)
	`, availCond, dupCond)

	var count int64
	if err := r.db.WithContext(ctx).Raw(sql, int64(catalogID), int64(catalogID)).Scan(&count).Error; err != nil {
		return 0, fmt.Errorf("count items: %w", err)
	}

	return count, nil
}

// --- Archive Management ---

// FindZipByPath finds a ZIP catalog by its path
func (r *CatalogRepository) FindZipByPath(ctx context.Context, path string) (*catalog.Catalog, error) {
	var model CatalogModel
	err := r.db.WithContext(ctx).
		Where("path = ? AND cat_type = ?", path, int(catalog.TypeZip)).
		First(&model).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find zip by path: %w", err)
	}

	return CatalogToDomain(&model), nil
}

// GetAllZipCatalogs returns all ZIP catalogs as path->ID map
func (r *CatalogRepository) GetAllZipCatalogs(ctx context.Context) (map[string]catalog.ID, error) {
	var models []CatalogModel

	err := r.db.WithContext(ctx).
		Where("cat_type = ?", int(catalog.TypeZip)).
		Find(&models).Error

	if err != nil {
		return nil, fmt.Errorf("get all zip catalogs: %w", err)
	}

	result := make(map[string]catalog.ID, len(models))
	for _, m := range models {
		result[m.Path] = catalog.ID(m.ID)
	}

	return result, nil
}

// DeleteWithBooks deletes catalogs and marks their books as deleted
func (r *CatalogRepository) DeleteWithBooks(ctx context.Context, catalogIDs []catalog.ID) (int64, error) {
	if len(catalogIDs) == 0 {
		return 0, nil
	}

	ids := make([]int64, len(catalogIDs))
	for i, id := range catalogIDs {
		ids[i] = int64(id)
	}

	var booksAffected int64

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Mark books as deleted
		result := tx.Model(&BookModel{}).
			Where("cat_id IN ?", ids).
			Update("avail", 0) // AvailabilityDeleted

		if result.Error != nil {
			return result.Error
		}
		booksAffected = result.RowsAffected

		// Recursively collect all descendant catalog IDs
		allIDs := make([]int64, len(ids))
		copy(allIDs, ids)

		for {
			var childIDs []int64
			if err := tx.Model(&CatalogModel{}).
				Where("parent_id IN ?", ids).
				Pluck("cat_id", &childIDs).Error; err != nil {
				return err
			}

			if len(childIDs) == 0 {
				break
			}

			// Mark books in children as deleted
			result := tx.Model(&BookModel{}).
				Where("cat_id IN ?", childIDs).
				Update("avail", 0)
			if result.Error != nil {
				return result.Error
			}
			booksAffected += result.RowsAffected

			allIDs = append(allIDs, childIDs...)
			ids = childIDs
		}

		// Delete all catalogs
		return tx.Delete(&CatalogModel{}, allIDs).Error
	})

	if err != nil {
		return 0, fmt.Errorf("delete with books: %w", err)
	}

	return booksAffected, nil
}

// Ensure CatalogRepository implements the interface
var _ repository.CatalogRepository = (*CatalogRepository)(nil)
