// Package matching provides shared string matching utilities for categorization
// rules and budget category matching.
//
// Issue #495: Extract common matching infrastructure to reduce duplication
// between internal/rules/evaluator.go and internal/services/budget_category_matching_service.go
package matching

import "strings"

// ContainsAll checks if slice a contains all elements of slice b.
// Case-insensitive comparison.
//
// Used by:
//   - ExistingCategoryEvaluator in rules/evaluator.go
//   - matchesCategory in services/budget_category_matching_service.go
func ContainsAll(a, b []string) bool {
	if len(b) == 0 {
		return true
	}

	aSet := make(map[string]bool)
	for _, item := range a {
		aSet[strings.ToLower(item)] = true
	}

	for _, item := range b {
		if !aSet[strings.ToLower(item)] {
			return false
		}
	}
	return true
}

// ContainsAny checks if slice a contains any element of slice b.
// Case-insensitive comparison. Returns false if either slice is empty/nil.
//
// Used by:
//   - matchesTags in services/budget_category_matching_service.go
//   - Potentially future tag matching in categorization rules
func ContainsAny(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}

	bSet := make(map[string]bool)
	for _, item := range b {
		bSet[strings.ToLower(item)] = true
	}

	for _, item := range a {
		if bSet[strings.ToLower(item)] {
			return true
		}
	}
	return false
}

// ExtractStringValues extracts string(s) from interface{}.
// Handles single string, []interface{}, or []string.
// Returns nil for unsupported types.
//
// Used by:
//   - Condition evaluators in rules/evaluator.go to handle flexible condition values
func ExtractStringValues(v interface{}) []string {
	switch val := v.(type) {
	case string:
		return []string{val}
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return val
	default:
		return nil
	}
}
