package httpapi

// Machine-readable error codes for consistent API error handling.
// Consumers can switch on `error.code` for programmatic error handling.
const (
	ErrCodeMethodNotAllowed  = "METHOD_NOT_ALLOWED"
	ErrCodeInvalidBody       = "INVALID_BODY"
	ErrCodeTrailingData      = "TRAILING_DATA"
	ErrCodeMissingField      = "MISSING_FIELD"
	ErrCodeInvalidThreshold  = "INVALID_THRESHOLD"
	ErrCodeInvalidEntityType = "INVALID_ENTITY_TYPE"
	ErrCodeInternal          = "INTERNAL_ERROR"
	ErrCodeUnauthorized      = "UNAUTHORIZED"
	ErrCodeForbidden         = "FORBIDDEN"
	ErrCodeNotFound          = "NOT_FOUND"
	ErrCodeConflict          = "CONFLICT"
	ErrCodeRateLimited       = "RATE_LIMITED"
	ErrCodeTooLarge          = "PAYLOAD_TOO_LARGE"
)
