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
	for i, entry := range data {
		if msg, ok := entry["message"].(string); ok {
			sanitized := sanitizeErrorMessage(msg)
			if sanitized != msg {
				data[i]["message"] = sanitized
				modified = true
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
func sanitizeErrorMessage(msg string) string {
	for _, pattern := range sensitivePatterns {
		msg = pattern.ReplaceAllString(msg, "[REDACTED]")
	}
	return msg
}
