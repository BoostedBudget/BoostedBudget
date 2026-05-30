package inputs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMCPContextInput_IsMCPOrigin(t *testing.T) {
	const secret = "test-secret-value"

	// Correct secret → trusted MCP Worker origin
	assert.True(t, (&MCPContextInput{MCPWorkerSecret: secret}).IsMCPOrigin(secret))

	// Wrong secret → not trusted
	assert.False(t, (&MCPContextInput{MCPWorkerSecret: "wrong"}).IsMCPOrigin(secret))

	// No secret sent → not trusted
	assert.False(t, (&MCPContextInput{}).IsMCPOrigin(secret))

	// Empty expected secret (misconfigured backend) → fail closed, never trust
	assert.False(t, (&MCPContextInput{MCPWorkerSecret: "anything"}).IsMCPOrigin(""))

	// Note: X-MCP-Origin header is NOT checked (Worker doesn't send it in backend API calls;
	// it's only set on the DO-internal request for routing. The secret alone proves identity.)
}
