package genre

// ID is a strongly typed identifier for genres
type ID int64

// Genre represents a book genre with hierarchical structure
type Genre struct {
	id         ID
	code       string // FB2 genre code (e.g., "sf_fantasy")
	section    string // Top-level category
	subsection string // Specific genre name
}

// New creates a new Genre
func New(code, section, subsection string) *Genre {
	return &Genre{
		code:       code,
		section:    section,
		subsection: subsection,
	}
}

// Reconstruct recreates a Genre from persistence layer
func Reconstruct(id ID, code, section, subsection string) *Genre {
	return &Genre{
		id:         id,
		code:       code,
		section:    section,
		subsection: subsection,
	}
}

// --- Domain Methods ---

// DisplayName returns the genre's display name
func (g *Genre) DisplayName() string {
	if g.subsection != "" {
		return g.subsection
	}
	return g.code
}

// FullPath returns section/subsection path
func (g *Genre) FullPath() string {
	if g.section == "" {
		return g.subsection
	}
	if g.subsection == "" {
		return g.section
	}
	return g.section + " / " + g.subsection
}

// BelongsToSection returns true if genre is in the given section
func (g *Genre) BelongsToSection(section string) bool {
	return g.section == section
}

// SetID sets the genre ID (used after persistence)
func (g *Genre) SetID(id ID) {
	g.id = id
}

// --- Getters ---

func (g *Genre) ID() ID           { return g.id }
func (g *Genre) Code() string       { return g.code }
func (g *Genre) Section() string    { return g.section }
func (g *Genre) Subsection() string { return g.subsection }
