package persistence

import (
	"github.com/sopds/sopds-go/internal/domain/repository"
)

// Repositories holds all repository implementations
type Repositories struct {
	Books     repository.BookRepository
	Authors   repository.AuthorRepository
	Genres    repository.GenreRepository
	Series    repository.SeriesRepository
	Catalogs  repository.CatalogRepository
	Bookshelf repository.BookshelfRepository
	Users     *UserRepository
}

// NewRepositories creates all repositories from a database connection
func NewRepositories(db *DB) *Repositories {
	return &Repositories{
		Books:     NewBookRepository(db),
		Authors:   NewAuthorRepository(db),
		Genres:    NewGenreRepository(db),
		Series:    NewSeriesRepository(db),
		Catalogs:  NewCatalogRepository(db),
		Bookshelf: NewBookshelfRepository(db),
		Users:     NewUserRepository(db.DB),
	}
}
