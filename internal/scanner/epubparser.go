package scanner

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/dimgord/sopds-go/internal/database"
)

// EPUB is a ZIP whose META-INF/container.xml points at an OPF file carrying
// Dublin Core metadata. We parse title/author/language/series so epub books get
// real metadata instead of a filename-derived title. Cover and genres are left
// empty on purpose: epub covers need manifest resolution + a server-side route,
// and epub <dc:subject> is freeform text that doesn't map to FB2 genre codes.
//
// XML tags below omit namespaces so they match by local name regardless of
// prefix (dc:title vs a default-namespaced <title>), which is robust across the
// many epub producers in the wild.

type epubContainer struct {
	Rootfiles []struct {
		FullPath  string `xml:"full-path,attr"`
		MediaType string `xml:"media-type,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opfPackage struct {
	Metadata struct {
		Title       []string     `xml:"title"`
		Creator     []opfCreator `xml:"creator"`
		Language    []string     `xml:"language"`
		Description []string     `xml:"description"`
		Date        []string     `xml:"date"`
		Meta        []opfMeta    `xml:"meta"`
	} `xml:"metadata"`
}

type opfCreator struct {
	Value  string `xml:",chardata"`
	FileAs string `xml:"file-as,attr"`
	Role   string `xml:"role,attr"`
}

type opfMeta struct {
	Name     string `xml:"name,attr"`     // epub2 / Calibre
	Content  string `xml:"content,attr"`  // epub2 / Calibre
	Property string `xml:"property,attr"` // epub3
	Refines  string `xml:"refines,attr"`  // epub3
	Value    string `xml:",chardata"`     // epub3 element text
}

// parseEPUB extracts metadata from EPUB bytes, returning an *FB2Metadata so
// callers share the existing FB2 handling. Returns an error if the bytes aren't
// a readable epub; the caller then falls back to the filename.
func parseEPUB(data []byte) (*FB2Metadata, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("epub: open zip: %w", err)
	}

	cxml, err := readZipEntry(zr, "META-INF/container.xml")
	if err != nil {
		return nil, fmt.Errorf("epub: container.xml: %w", err)
	}
	var cont epubContainer
	if err := xml.Unmarshal(cxml, &cont); err != nil {
		return nil, fmt.Errorf("epub: parse container.xml: %w", err)
	}
	opfPath := ""
	for _, rf := range cont.Rootfiles {
		if strings.TrimSpace(rf.FullPath) != "" {
			opfPath = rf.FullPath
			break
		}
	}
	if opfPath == "" {
		return nil, fmt.Errorf("epub: no rootfile in container.xml")
	}

	opfData, err := readZipEntry(zr, opfPath)
	if err != nil {
		return nil, fmt.Errorf("epub: opf %q: %w", opfPath, err)
	}
	var pkg opfPackage
	if err := xml.Unmarshal(opfData, &pkg); err != nil {
		return nil, fmt.Errorf("epub: parse opf: %w", err)
	}

	meta := &FB2Metadata{}
	if len(pkg.Metadata.Title) > 0 {
		meta.Title = strings.TrimSpace(pkg.Metadata.Title[0])
	}
	if len(pkg.Metadata.Language) > 0 {
		meta.Lang = strings.TrimSpace(pkg.Metadata.Language[0])
	}
	if len(pkg.Metadata.Description) > 0 {
		meta.Annotation = strings.TrimSpace(pkg.Metadata.Description[0])
	}
	if len(pkg.Metadata.Date) > 0 {
		meta.DocDate = strings.TrimSpace(pkg.Metadata.Date[0])
	}
	for _, c := range pkg.Metadata.Creator {
		role := strings.TrimSpace(c.Role)
		if role != "" && role != "aut" {
			continue // skip explicit editors/illustrators/translators
		}
		a := creatorToAuthor(c)
		if a.FirstName != "" || a.LastName != "" {
			meta.Authors = append(meta.Authors, a)
		}
	}
	if name, idx := epubSeries(pkg.Metadata.Meta); name != "" {
		meta.Series = []SeriesInfo{{Name: name, Number: idx}}
	}
	return meta, nil
}

// creatorToAuthor splits an OPF <dc:creator> into first/last. Prefer the
// opf:file-as "Last, First" sort form; otherwise treat the last word as surname
// (same convention as parseAuthorFullName).
func creatorToAuthor(c opfCreator) database.Author {
	if fa := strings.TrimSpace(c.FileAs); strings.Contains(fa, ",") {
		parts := strings.SplitN(fa, ",", 2)
		last := strings.TrimSpace(parts[0])
		first := strings.TrimSpace(parts[1])
		if last != "" || first != "" {
			return database.Author{FirstName: first, LastName: last}
		}
	}
	return parseAuthorFullName(strings.TrimSpace(c.Value))
}

// epubSeries reads series name + index from Calibre (epub2) meta first, then
// falls back to the epub3 belongs-to-collection / group-position form.
func epubSeries(metas []opfMeta) (string, int) {
	var name string
	var idx int
	for _, m := range metas {
		switch m.Name {
		case "calibre:series":
			name = strings.TrimSpace(m.Content)
		case "calibre:series_index":
			idx = parseSeriesIndex(m.Content)
		}
	}
	if name != "" {
		return name, idx
	}
	for _, m := range metas {
		switch strings.TrimSpace(m.Property) {
		case "belongs-to-collection":
			name = strings.TrimSpace(m.Value)
		case "group-position":
			idx = parseSeriesIndex(m.Value)
		}
	}
	return name, idx
}

// parseSeriesIndex turns "3" or "3.0" into 3 (0 if unparseable).
func parseSeriesIndex(s string) int {
	s = strings.TrimSpace(s)
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		s = s[:dot]
	}
	n, _ := strconv.Atoi(s)
	return n
}

func readZipEntry(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("entry %q not found", name)
}

// parseEPUBReader reads an epub from a stream (zip/7z entry) and parses it.
func parseEPUBReader(r io.Reader) (*FB2Metadata, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return parseEPUB(data)
}

// parseEPUBFile parses a loose .epub on disk, setting book.Title from metadata
// (falling back to the filename) and returning the metadata for author/series
// linking. Mirrors parseFB2MetadataFull.
func (s *Scanner) parseEPUBFile(path string, book *database.Book) *FB2Metadata {
	fallback := strings.TrimSuffix(book.Filename, ".epub")
	data, err := os.ReadFile(path)
	if err != nil {
		book.Title = fallback
		return nil
	}
	meta, err := parseEPUB(data)
	if err != nil {
		log.Printf("Failed to parse EPUB %s: %v", path, err)
		book.Title = fallback
		return nil
	}
	book.Title = meta.Title
	if book.Title == "" {
		book.Title = fallback
	}
	book.Annotation = meta.Annotation
	book.Lang = meta.Lang
	book.DocDate = meta.DocDate
	return meta
}
