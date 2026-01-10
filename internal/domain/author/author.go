package author

import "strings"

// ID is a strongly typed identifier for authors
type ID int64

// Author represents a book author
type Author struct {
	id        ID
	firstName string
	lastName  string
}

// New creates a new Author
func New(firstName, lastName string) *Author {
	return &Author{
		firstName: strings.TrimSpace(firstName),
		lastName:  strings.TrimSpace(lastName),
	}
}

// Reconstruct recreates an Author from persistence layer
func Reconstruct(id ID, firstName, lastName string) *Author {
	return &Author{
		id:        id,
		firstName: firstName,
		lastName:  lastName,
	}
}

// --- Domain Methods ---

// FullName returns the author's display name (Last First format)
func (a *Author) FullName() string {
	if a.firstName == "" {
		return a.lastName
	}
	if a.lastName == "" {
		return a.firstName
	}
	return a.lastName + " " + a.firstName
}

// DisplayName returns the author's name in First Last format
func (a *Author) DisplayName() string {
	if a.firstName == "" {
		return a.lastName
	}
	if a.lastName == "" {
		return a.firstName
	}
	return a.firstName + " " + a.lastName
}

// SortKey returns a string suitable for alphabetical sorting
func (a *Author) SortKey() string {
	return strings.ToLower(a.lastName + " " + a.firstName)
}

// IsUnknown returns true if this is the "unknown author" placeholder
func (a *Author) IsUnknown() bool {
	return a.firstName == "" && a.lastName == ""
}

// Matches returns true if this author matches the given names
func (a *Author) Matches(firstName, lastName string) bool {
	return strings.EqualFold(a.firstName, firstName) &&
		strings.EqualFold(a.lastName, lastName)
}

// SetID sets the author ID (used after persistence)
func (a *Author) SetID(id ID) {
	a.id = id
}

// --- Getters ---

func (a *Author) ID() ID          { return a.id }
func (a *Author) FirstName() string { return a.firstName }
func (a *Author) LastName() string  { return a.lastName }
