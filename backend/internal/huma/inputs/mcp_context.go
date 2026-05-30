package inputs

// MCPContextInput reads MCP-origin headers injected by the MCP Cloudflare Worker.
// Only present when the request originated from the Worker (X-MCP-Origin: 1).
// Security: these headers are trusted because the CloudflareOnly middleware already
// validated X-Origin-Secret + Cloudflare IP for all incoming requests.
type MCPContextInput struct {
	MCPOrigin  string `header:"X-MCP-Origin" doc:"Set by MCP Worker to enable transactional audit"`
	MCPGrantID string `header:"X-MCP-Grant-Id" doc:"OAuth grant ID from the MCP Worker auth context"`
}

func (m *MCPContextInput) IsMCPOrigin() bool {
	return m.MCPOrigin == "1"
}
