package inputs

import "crypto/subtle"

// MCPContextInput reads MCP-origin headers injected by the MCP Cloudflare Worker.
//
// Architecture note: the Worker (index.ts) forwards X-MCP-Origin/X-MCP-Grant-Id/
// X-MCP-Access-Token to the Durable Object for DO-internal routing. Backend API calls
// from the DO go through fetch-mutator.ts which ONLY injects X-MCP-Worker-Secret and
// X-MCP-Grant-Id — NOT X-MCP-Origin. Therefore IsMCPOrigin() must NOT check X-MCP-Origin;
// X-MCP-Worker-Secret IS the sole proof of Worker identity (second independent secret,
// separate from X-Origin-Secret which gates all CF traffic).
//
// IMPORTANT: Adding or removing fields here requires running `make generate-openapi`
// to keep api/openapi.yaml in sync with the Huma-generated spec.
type MCPContextInput struct {
	MCPGrantID      string `header:"X-MCP-Grant-Id" doc:"OAuth grant ID forwarded by the MCP Worker from the verified auth context"`
	MCPWorkerSecret string `header:"X-MCP-Worker-Secret" doc:"Worker-only shared secret; constant-time validated to prove the request came from the MCP Worker, not an arbitrary client"`
}

// IsMCPOrigin returns true when X-MCP-Worker-Secret matches expectedSecret in constant
// time (prevents timing attacks). The Worker is the sole injector of X-MCP-Worker-Secret;
// it does not send X-MCP-Origin in backend API calls, so we check only the secret.
// If expectedSecret is empty (misconfigured backend), returns false — fail closed.
//
// Design note — silent downgrade on failure (intentional):
// When IsMCPOrigin() returns false (missing/wrong secret), the handler falls through to
// the non-MCP code path (no transactional audit, normal idempotency). This is a deliberate
// mode-selector, NOT a hard gate. A request with MCP markers but wrong secret still succeeds
// as a regular write — it simply won't be audited via the MCP path.
// This design keeps the audit path opt-in (Worker must actively assert identity) rather than
// rejecting writes outright, which would break non-Worker callers.
func (m *MCPContextInput) IsMCPOrigin(expectedSecret string) bool {
	if expectedSecret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(m.MCPWorkerSecret), []byte(expectedSecret)) == 1
}
