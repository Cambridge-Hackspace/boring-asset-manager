package models

import "time"

// Related terms (docs/data-model.md#related-terms) are a graph the
// instance keeps of what things are called, shaped like W3C SKOS: a term
// has a preferred label and alternative labels, and terms link as broader
// and narrower, or as related. Search expands a term to its other names
// and everything narrower than it.

// The relations between terms. A link is stored once: broader from the
// narrower term to the broader one, and related from the term whose ID
// sorts first.
const (
	TermBroader  = "broader"
	TermNarrower = "narrower"
	TermRelated  = "related"
)

// TermSource is who added a term or a link, and from where: a person or
// an API token (AddedByToken), through an assistant over MCP when Via
// names it, or as part of an import (ImportID).
type TermSource struct {
	AddedBy      string `json:"added_by,omitempty"`
	AddedByToken bool   `json:"added_by_token,omitempty"`
	// AddedByName is the person's or token's name, as they are now.
	AddedByName string `json:"added_by_name,omitempty"`
	Via         string `json:"via,omitempty"`
	ImportID    string `json:"import_id,omitempty"`
}

// Kind is where it came from: "import", "agent" (an assistant over MCP)
// or "person" (a person, or an API token, directly).
func (s TermSource) Kind() string {
	switch {
	case s.ImportID != "":
		return "import"
	case s.Via != "":
		return "agent"
	}
	return "person"
}

// Term is a thing as people call it: its preferred label, the other names
// it goes by, and a note on what it means.
type Term struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	AltLabels []string `json:"alt_labels"`
	Note      string   `json:"note"`
	TermSource
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TermLink joins two terms: TermID is narrower than OtherID (broader), or
// the two are related.
type TermLink struct {
	TermID   string `json:"term_id"`
	OtherID  string `json:"other_id"`
	Relation string `json:"relation"`
	TermSource
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

// TermImport is a batch of terms and links added together, from a file
// or a thesaurus, so that it can be reviewed, and removed, as one.
type TermImport struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	TermSource
	Terms     int       `json:"terms"`
	Links     int       `json:"links"`
	CreatedAt time.Time `json:"created_at"`
}
