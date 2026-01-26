package book

import "errors"

// Domain errors
var (
	ErrEmptyFilename   = errors.New("filename cannot be empty")
	ErrInvalidFilesize = errors.New("filesize cannot be negative")
	ErrSelfDuplicate   = errors.New("book cannot be a duplicate of itself")
	ErrInvalidFormat   = errors.New("invalid book format")
)

// Format represents a book file format
type Format string

const (
	// Ebook formats
	FormatFB2     Format = "fb2"
	FormatEPUB    Format = "epub"
	FormatMOBI    Format = "mobi"
	FormatPDF     Format = "pdf"
	FormatDJVU    Format = "djvu"
	// Audio formats
	FormatMP3  Format = "mp3"
	FormatM4B  Format = "m4b"
	FormatM4A  Format = "m4a"
	FormatFLAC Format = "flac"
	FormatOGG  Format = "ogg"
	FormatOPUS Format = "opus"
	// Unknown
	FormatUnknown Format = ""
)

// ParseFormat parses a string into a Format
func ParseFormat(s string) Format {
	switch s {
	case "fb2":
		return FormatFB2
	case "epub":
		return FormatEPUB
	case "mobi":
		return FormatMOBI
	case "pdf":
		return FormatPDF
	case "djvu":
		return FormatDJVU
	case "mp3":
		return FormatMP3
	case "m4b":
		return FormatM4B
	case "m4a":
		return FormatM4A
	case "flac":
		return FormatFLAC
	case "ogg":
		return FormatOGG
	case "opus":
		return FormatOPUS
	default:
		return Format(s)
	}
}

// String returns the format as a string
func (f Format) String() string {
	return string(f)
}

// IsConvertible returns true if the format can be converted to other formats
func (f Format) IsConvertible() bool {
	return f == FormatFB2
}

// IsAudio returns true if this is an audio format
func (f Format) IsAudio() bool {
	switch f {
	case FormatMP3, FormatM4B, FormatM4A, FormatFLAC, FormatOGG, FormatOPUS:
		return true
	}
	return false
}

// IsEbook returns true if this is an ebook format
func (f Format) IsEbook() bool {
	switch f {
	case FormatFB2, FormatEPUB, FormatMOBI, FormatPDF, FormatDJVU:
		return true
	}
	return false
}

// Language represents a book language code
type Language string

const (
	LangRussian Language = "ru"
	LangEnglish Language = "en"
	LangUnknown Language = ""
)

// String returns the language code
func (l Language) String() string {
	return string(l)
}

// IsUnknown returns true if language is not set
func (l Language) IsUnknown() bool {
	return l == "" || l == LangUnknown
}

// Availability represents the book's availability status
type Availability int

const (
	AvailabilityDeleted  Availability = 0
	AvailabilityPending  Availability = 1
	AvailabilityVerified Availability = 2
)

// String returns a human-readable status
func (a Availability) String() string {
	switch a {
	case AvailabilityDeleted:
		return "deleted"
	case AvailabilityPending:
		return "pending"
	case AvailabilityVerified:
		return "available"
	default:
		return "unknown"
	}
}

// IsAvailable returns true if book is accessible
func (a Availability) IsAvailable() bool {
	return a != AvailabilityDeleted
}

// CatalogType represents the type of catalog/storage
type CatalogType int

const (
	CatalogTypeNormal CatalogType = 0
	CatalogTypeZip    CatalogType = 1
	CatalogTypeGzip   CatalogType = 2
)

// String returns a string representation
func (t CatalogType) String() string {
	switch t {
	case CatalogTypeNormal:
		return "directory"
	case CatalogTypeZip:
		return "zip"
	case CatalogTypeGzip:
		return "gzip"
	default:
		return "unknown"
	}
}

// IsArchive returns true if this is an archive type
func (t CatalogType) IsArchive() bool {
	return t == CatalogTypeZip || t == CatalogTypeGzip
}

// Cover represents book cover information
type Cover struct {
	data        string // base64 or reference
	contentType string
}

// NewCover creates a new Cover value object
func NewCover(data, contentType string) Cover {
	return Cover{
		data:        data,
		contentType: contentType,
	}
}

// EmptyCover returns an empty cover
func EmptyCover() Cover {
	return Cover{}
}

// HasCover returns true if cover data is available
func (c Cover) HasCover() bool {
	return c.data != ""
}

// Data returns the cover data
func (c Cover) Data() string {
	return c.data
}

// ContentType returns the MIME type
func (c Cover) ContentType() string {
	return c.contentType
}
