package tenant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// ErrNotFound is returned when a tenant or key does not exist.
var ErrNotFound = errors.New("tenant: not found")

// ErrInvalidTier is returned when an unknown permission tier is used.
var ErrInvalidTier = errors.New("tenant: invalid tier")

// Tier represents the permission level of a tenant's API key.
type Tier string

const (
	// TierScreeningOnly allows /screen but not review/case APIs.
	TierScreeningOnly Tier = "screening_only"
	// TierFull allows screening plus review queue and watchlist management.
	TierFull Tier = "full"
)

// ValidTiers lists all accepted tiers.
var ValidTiers = []Tier{TierScreeningOnly, TierFull}

// Tenant is a customer of the screening service.
type Tenant struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	APIKeyHash string `json:"-"` // sha256 hex of the raw API key; never expose raw key
	Tier       Tier   `json:"tier"`
	// LLMCascadeEnabled controls whether this tenant's borderline matches may
	// be sent to a third-party LLM for verification. DEFAULT OFF (privacy:
	// PII must not leave our infra unless the customer explicitly opts in).
	LLMCascadeEnabled bool      `json:"llm_cascade_enabled"`
	CreatedAt         time.Time `json:"created_at"`
}

// Store persists tenants and looks up API keys.
type Store interface {
	// CreateTenant inserts a new tenant. Returns the tenant with a fresh ID.
	CreateTenant(ctx context.Context, name string, rawAPIKey string, tier Tier) (*Tenant, error)
	// LookupByAPIKey finds a tenant by the sha256 hash of their API key.
	LookupByAPIKey(ctx context.Context, rawAPIKey string) (*Tenant, error)
	// ListTenants returns all tenants (hash omitted).
	ListTenants(ctx context.Context) ([]Tenant, error)
	// SetLLMCascade enables or disables the optional LLM cascade for a tenant.
	SetLLMCascade(ctx context.Context, tenantID string, enabled bool) error
	// Close releases the underlying store.
	Close() error
}

// HashAPIKey returns the hex sha256 of a raw API key. Storing only the hash
// means a DB leak does not expose usable credentials.
func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ValidateTier returns true if tier is a known permission level.
func ValidateTier(t Tier) bool {
	for _, v := range ValidTiers {
		if t == v {
			return true
		}
	}
	return false
}
