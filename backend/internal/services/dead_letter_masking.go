package services

import (
	"encoding/json"
	"regexp"
	"strings"
)

// sensitiveArgKeys are top-level JSON keys whose string values should be redacted
// in admin UI to prevent leaking Plaid tokens, passwords, or other secrets.
var sensitiveArgKeys = map[string]bool{
	"access_token": true,
	"token":        true,
	"email":        true,
	"password":     true,
	"secret":       true,
	"api_key":      true,
	"private_key":  true,
}

// sensitivePatterns are regexp patterns applied to error messages to redact
// sensitive substrings like tokens, file paths, or connection strings.
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`token=[^\s&"]+`),
	regexp.MustCompile(`password=[^\s&"]+`),
	regexp.MustCompile(`secret=[^\s&"]+`),
}

// MaskJobArgs redacts known sensitive fields in a job args JSON object.
// Returns the original bytes unchanged if parsing fails or input is nil.
func MaskJobArgs(args json.RawMessage) json.RawMessage {
	if args == nil {
		return nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(args, &data); err != nil {
		return args // return original on parse failure
	}

	modified := false
	for key := range data {
		if sensitiveArgKeys[strings.ToLower(key)] {
			if _, isStr := data[key].(string); isStr {
				data[key] = "[REDACTED]"
				modified = true
			}
		}
	}

	if !modified {
		return args
	}

	masked, err := json.Marshal(data)
	if err != nil {
		return args
	}
	return masked
}

// MaskJobErrors sanitizes error message strings in a JSON array of job errors.
// Each element is expected to be {"attempt":N,"message":"...","timestamp":"..."}.
// Returns the original bytes unchanged if parsing fails or input is nil.
func MaskJobErrors(errors json.RawMessage) json.RawMessage {
	if errors == nil {
		return nil
	}

	var data []map[string]interface{}
	if err := json.Unmarshal(errors, &data); err != nil {
		return errors
	}

	modified := false
	for _, entry := range data {
		if maskStringField(entry, "message") {
			modified = true
		}
		// #1631 added two optional keys carrying the SAME free text this function already
		// scrubs, in a richer shape: `chain` (the wrapping layers) and `pg_error` (what
		// Postgres sent). An old-shape row has neither and falls through untouched.
		if maskStringSlice(entry, "chain") {
			modified = true
		}
		if pg, ok := entry["pg_error"].(map[string]interface{}); ok {
			// Only the three free-text fields. code/constraint_name/table_name/schema_name/
			// column_name/severity/routine are schema identifiers, never credentials, and
			// scrubbing them would destroy the signal this feature exists to surface.
			for _, key := range []string{"message", "detail", "hint"} {
				if maskStringField(pg, key) {
					modified = true
				}
			}
		}
	}

	if !modified {
		return errors
	}

	masked, err := json.Marshal(data)
	if err != nil {
		return errors
	}
	return masked
}

// sanitizeErrorMessage replaces known sensitive patterns in error message strings.
// maskStringField sanitizes obj[key] in place when it holds a string, reporting whether it
// changed. A missing key or a non-string value is a no-op, which is what keeps old-shape rows
// byte-identical.
func maskStringField(obj map[string]interface{}, key string) bool {
	msg, ok := obj[key].(string)
	if !ok {
		return false
	}
	sanitized := sanitizeErrorMessage(msg)
	if sanitized == msg {
		return false
	}
	obj[key] = sanitized
	return true
}

// maskStringSlice sanitizes every string element of obj[key] in place.
func maskStringSlice(obj map[string]interface{}, key string) bool {
	items, ok := obj[key].([]interface{})
	if !ok {
		return false
	}
	modified := false
	for i, item := range items {
		msg, isStr := item.(string)
		if !isStr {
			continue
		}
		sanitized := sanitizeErrorMessage(msg)
		if sanitized != msg {
			items[i] = sanitized
			modified = true
		}
	}
	return modified
}

func sanitizeErrorMessage(msg string) string {
	for _, pattern := range sensitivePatterns {
		msg = pattern.ReplaceAllString(msg, "[REDACTED]")
	}
	return msg
}
