package models

import "time"

// EntityType represents the category of entity being screened.
type EntityType string

const (
	EntityTypeIndividual   EntityType = "individual"
	EntityTypeOrganization EntityType = "organization"
	EntityTypeVessel       EntityType = "vessel"
	EntityTypeAircraft     EntityType = "aircraft"
)

// ScreeningStatus is the overall disposition of a screening.
type ScreeningStatus string

const (
	// StatusPass means no qualifying match was found.
	StatusPass ScreeningStatus = "pass"
	// StatusHit means at least one candidate matched at or above threshold.
	StatusHit ScreeningStatus = "hit"
	// StatusPending means a high-risk match or LLM-uncertain candidate
	// requires human review before a final disposition.
	StatusPending ScreeningStatus = "pending"
)

// Sensitivity indicates how sensitive a matched record is. Used to control
// how much detail downstream systems receive.
type Sensitivity string

const (
	// SensitivityNormal is the default (no special handling).
	SensitivityNormal Sensitivity = "normal"
	// SensitivitySanctionsRelated marks sanctions-list matches (SDN etc).
	SensitivitySanctionsRelated Sensitivity = "sanctions_related"
)

// SanctionBasis explains WHY a record is sanctioned.
type SanctionBasis string

const (
	// BasisDirect means the entity itself is on the list.
	BasisDirect SanctionBasis = "direct"
	// BasisOwnership50Rule means the entity is ≥50% owned by a sanctioned entity.
	BasisOwnership50Rule SanctionBasis = "ownership_50_rule"
)

// ScreeningRequest is the input payload for a screening operation.
type ScreeningRequest struct {
	QueryName          string     `json:"query_name"`
	EntityType         EntityType `json:"entity_type,omitempty"`
	Country            string     `json:"country,omitempty"`
	DateOfBirth        string     `json:"date_of_birth,omitempty"`
	RegistrationNumber string     `json:"registration_number,omitempty"`
	Fuzzy              bool       `json:"fuzzy,omitempty"`
	Threshold          float64    `json:"threshold,omitempty"`
}

// ScreeningResult is the full response returned after screening.
type ScreeningResult struct {
	RequestID    string               `json:"request_id"`
	ScreenedAt   time.Time            `json:"screened_at"`
	Status       ScreeningStatus      `json:"status"`
	TotalMatches int                  `json:"total_matches"`
	Candidates   []ScreeningCandidate `json:"candidates"`
	// DataVersion is the sanctions-list version used for this screening
	// (e.g. "2026-08-07"). Lets compliance users verify list freshness.
	DataVersion string `json:"data_version"`
	// SourcesCovered lists which lists were included (OFAC-SDN, EU-Consolidated, UN-Consolidated).
	SourcesCovered []string `json:"sources_covered"`
}

// ScreeningCandidate represents a single potential match found
// against a sanctions list entry.
type ScreeningCandidate struct {
	EntityName      string            `json:"entity_name"`
	EntityType      EntityType        `json:"entity_type"`
	ListName        string            `json:"list_name"`
	ListVersion     string            `json:"list_version"`
	ConfidenceScore float64           `json:"confidence_score"`
	MatchedFields   []string          `json:"matched_fields"`
	Sources         []SourceReference `json:"sources"`
	Explanation     *MatchExplanation `json:"explanation,omitempty"`
	LLMVerification *LLMVerification  `json:"llm_verification,omitempty"`
	Sensitivity     Sensitivity       `json:"sensitivity"`
	SanctionBasis   SanctionBasis     `json:"sanction_basis"`
}

// SourceReference identifies the origin of a sanctions listing.
type SourceReference struct {
	SourceName string `json:"source_name"`
	ListURL    string `json:"list_url,omitempty"`
	RefNumber  string `json:"ref_number,omitempty"`
	Country    string `json:"country,omitempty"`
}

// MatchExplanation describes why a candidate was flagged as a match,
// providing transparency for audit and compliance review.
type MatchExplanation struct {
	Summary          string             `json:"summary"`
	MatchType        string             `json:"match_type"`
	MatchedOn        []string           `json:"matched_on"`
	ScoringBreakdown map[string]float64 `json:"scoring_breakdown,omitempty"`
}

// LLMVerification records the result of an LLM cascade check.
// Present on candidates that were escalated to an LLM for verification.
type LLMVerification struct {
	Verified  bool      `json:"verified"`
	Reasoning string    `json:"reasoning,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}
