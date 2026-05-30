package inputs

import "crypto/subtle"

// MCPContextInput reads MCP-origin headers injected by the MCP Cloudflare Worker.
// Only present when the request originated from the Worker (X-MCP-Origin: 1).
//
// Security: X-MCP-Origin alone is not sufficient — any authenticated client can set it.
// IsMCPOrigin() performs a constant-time comparison of X-MCP-Worker-Secret against the
// configured backend secret to verify the request genuinely came from the Worker.
type MCPContextInput struct {
	MCPOrigin       string `header:"X-MCP-Origin" doc:"Set by MCP Worker to enable transactional audit"`
	MCPGrantID      string `header:"X-MCP-Grant-Id" doc:"OAuth grant ID from the MCP Worker auth context"`
	MCPWorkerSecret string `header:"X-MCP-Worker-Secret" doc:"Worker-only shared secret; validated backend-side to prevent X-MCP-Origin spoofing"`
}

// IsMCPOrigin returns true only when X-MCP-Origin is "1" AND X-MCP-Worker-Secret matches
// expectedSecret in constant time (prevents timing attacks).
// If expectedSecret is empty (misconfigured backend), returns false — fail closed.
func (m *MCPContextInput) IsMCPOrigin(expectedSecret string) bool {
	if expectedSecret == "" {
		return false
	}
	return m.MCPOrigin == "1" &&
		subtle.ConstantTimeCompare([]byte(m.MCPWorkerSecret), []byte(expectedSecret)) == 1
}
