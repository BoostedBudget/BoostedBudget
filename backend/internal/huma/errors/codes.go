package errors

// Error code constants for RFC 9457 error responses.
// These are used in the "code" field of error responses to allow
// clients to programmatically handle specific error conditions.
const (
	CodeNotFound     = "RESOURCE_NOT_FOUND"
	CodeConflict     = "RESOURCE_ALREADY_EXISTS"
	CodeValidation   = "VALIDATION_ERROR"
	CodeTierLimit    = "TIER_LIMIT_EXCEEDED"
	CodeRateLimit    = "RATE_LIMIT_EXCEEDED"
	CodeServiceDown  = "EXTERNAL_SERVICE_UNAVAILABLE"
	CodeUnauthorized = "UNAUTHORIZED"
	CodeInternal     = "INTERNAL_ERROR"
)
