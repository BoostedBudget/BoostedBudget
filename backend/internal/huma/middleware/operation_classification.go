package middleware

import "github.com/danielgtaylor/huma/v2"

// ReadEquivalentOperationMetadataKey marks an authenticated non-GET operation that observes or
// reports existing activity without mutating domain state. Read-equivalent operations remain
// available when subscription status is read-only or temporarily unresolved and do not invalidate
// financial-data caches.
//
// Keep this classification narrow. It bypasses two protections that every domain mutation needs,
// so a route must opt in explicitly at registration rather than matching a path or method loosely.
const ReadEquivalentOperationMetadataKey = "boostedbudget.read-equivalent"

// IsReadEquivalentOperation is the shared policy check for read-only enforcement and cache
// invalidation. Only the literal boolean true opts in; missing or malformed metadata fails closed.
func IsReadEquivalentOperation(operation *huma.Operation) bool {
	if operation == nil {
		return false
	}
	readEquivalent, ok := operation.Metadata[ReadEquivalentOperationMetadataKey].(bool)
	return ok && readEquivalent
}
