package domain

// MCPOperationClass classifies an operation's capability for the MCP server.
type MCPOperationClass string

const (
	MCPClassRead  MCPOperationClass = "read"
	MCPClassWrite MCPOperationClass = "write"
	// MCPClassDestructive marks an operation that permanently destroys stored rows
	// (no tombstone, no restore path) — the same distinction the TypeScript capability
	// manifest (apps/mcp-server/src/manifest/capability-manifest.ts) draws between
	// 'write' (tombstoning) and 'destructive' (Issue #1569 Task 16). Still subject to
	// the same v1 recipe rule as MCPClassWrite: MCPOperationIsRead is false for it, so
	// recipe_service.go rejects it — the distinction exists for classification fidelity
	// with the TS source of truth, not to grant it anything MCPClassWrite doesn't have.
	MCPClassDestructive MCPOperationClass = "destructive"
	MCPClassExcluded    MCPOperationClass = "excluded"
)

// MCPOperationManifest classifies every known operationId and is defined in
// mcp_manifest_generated.go — generated from the TypeScript capability manifest via
// apps/mcp-server/src/spec/generated/spec-index.json. See that file's header for how
// to regenerate it. Do not hand-edit the map; add/change entries in
// apps/mcp-server/src/manifest/capability-manifest.ts instead and regenerate.

// MCPOperationAllowed returns true if the operationId is allowed through MCP
// (present in the manifest and not excluded).
func MCPOperationAllowed(operationID string) bool {
	class, ok := MCPOperationManifest[operationID]
	return ok && class != MCPClassExcluded
}

// MCPOperationIsRead returns true if the operation is a read-class operation.
func MCPOperationIsRead(operationID string) bool {
	return MCPOperationManifest[operationID] == MCPClassRead
}
