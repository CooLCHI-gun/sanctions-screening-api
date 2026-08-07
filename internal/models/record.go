package models

import "time"

// SanctionsRecord is the unified normalized representation of a
// single entity entry from any sanctions list source.
type SanctionsRecord struct {
	ID               string             `json:"id"`
	SourceRecordID   string             `json:"source_record_id"`
	ListName         string             `json:"list_name"`
	ListVersion      string             `json:"list_version,omitempty"`
	IssuingAuthority string             `json:"issuing_authority"`
	SourceURL        string             `json:"source_url,omitempty"`
	LastUpdated      time.Time          `json:"last_updated,omitempty"`
	EntityType       EntityType         `json:"entity_type"`
	Program          string             `json:"program,omitempty"`
	Country          string             `json:"country,omitempty"`
	Names            []RecordName       `json:"names"`
	Aliases          []RecordAlias      `json:"aliases,omitempty"`
	Identifiers      []RecordIdentifier `json:"identifiers,omitempty"`
	Dates            []RecordDate       `json:"dates,omitempty"`
	Addresses        []RecordAddress    `json:"addresses,omitempty"`
}

// RecordName holds a single name for a sanctions entity.
type RecordName struct {
	FullName  string `json:"full_name"`
	LastName  string `json:"last_name,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	Language  string `json:"language,omitempty"`
	Script    string `json:"script,omitempty"`
}

// RecordAlias holds an alternative name for the entity.
type RecordAlias struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"` // e.g. "aka", "fka"
}

// RecordIdentifier holds a document or registration identifier.
type RecordIdentifier struct {
	Type    string `json:"type"` // e.g. "passport", "national_id", "registration"
	Value   string `json:"value"`
	Country string `json:"country,omitempty"`
}

// RecordDate holds a date associated with the entity.
type RecordDate struct {
	Type  string `json:"type"`  // e.g. "date_of_birth", "date_of_death"
	Value string `json:"value"` // ISO 8601 date string
}

// RecordAddress holds a physical address.
type RecordAddress struct {
	Address string `json:"address"`
	Country string `json:"country,omitempty"`
}

// RecordSourceMeta describes the provenance of a sanctions list.
type RecordSourceMeta struct {
	ListName         string    `json:"list_name"`
	IssuingAuthority string    `json:"issuing_authority"`
	SourceURL        string    `json:"source_url,omitempty"`
	Version          string    `json:"version,omitempty"`
	LastUpdated      time.Time `json:"last_updated,omitempty"`
}
