package persistence

import (
	"context"

	"gorm.io/gorm"

	"github.com/dimgord/sopds-go/internal/database"
)

// SeriesLink is a resolved series membership (series id + number in series).
type SeriesLink struct {
	SeriesID int64
	SerNo    int
}

// GetEpubZipCatalogPaths returns the distinct catalog paths (relative zip paths)
// of ZIP archives that contain at least one epub book. Used by --epub-only to
// visit only the archives worth re-parsing.
func (s *Service) GetEpubZipCatalogPaths(ctx context.Context) ([]string, error) {
	var paths []string
	err := s.GORM().WithContext(ctx).
		Raw(`SELECT DISTINCT c.path
		       FROM catalogs c
		       JOIN books b ON b.cat_id = c.cat_id
		      WHERE c.cat_type = ? AND b.format = 'epub' AND b.is_audiobook = false`,
			database.CatZip).
		Scan(&paths).Error
	return paths, err
}

// RefreshBookMetadata applies freshly-parsed metadata to an already-catalogued
// book: it updates the core fields and REPLACES the author and series links, all
// in one transaction. The book_id is kept, so bookshelf/favorite/TTS references
// survive. Used by --epub-only to re-apply EPUB OPF metadata in place (a normal
// rescan only marks existing books verified, it does not re-read them).
func (s *Service) RefreshBookMetadata(ctx context.Context, bookID int64, title, lang, annotation, docdate string, authorIDs []int64, series []SeriesLink) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("books").Where("book_id = ?", bookID).
			Updates(map[string]interface{}{
				"title":      title,
				"lang":       lang,
				"annotation": annotation,
				"docdate":    docdate,
			}).Error; err != nil {
			return err
		}

		if err := tx.Exec("DELETE FROM bauthors WHERE book_id = ?", bookID).Error; err != nil {
			return err
		}
		for _, aid := range authorIDs {
			if err := tx.Exec(
				"INSERT INTO bauthors (book_id, author_id, role) VALUES (?, ?, 'author') ON CONFLICT DO NOTHING",
				bookID, aid).Error; err != nil {
				return err
			}
		}

		if err := tx.Exec("DELETE FROM bseries WHERE book_id = ?", bookID).Error; err != nil {
			return err
		}
		for _, sl := range series {
			if err := tx.Exec(
				"INSERT INTO bseries (book_id, ser_id, ser_no) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
				bookID, sl.SeriesID, sl.SerNo).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
