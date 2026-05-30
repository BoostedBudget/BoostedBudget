package inputs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMCPContextInput_IsMCPOrigin(t *testing.T) {
	const secret = "test-secret-value"

	// Both header and secret match → trusted MCP origin
	assert.True(t, (&MCPContextInput{MCPOrigin: "1", MCPWorkerSecret: secret}).IsMCPOrigin(secret))

	// Header is "1" but secret is wrong → not trusted
	assert.False(t, (&MCPContextInput{MCPOrigin: "1", MCPWorkerSecret: "wrong"}).IsMCPOrigin(secret))

	// Header is "1" but no secret sent → not trusted
	assert.False(t, (&MCPContextInput{MCPOrigin: "1"}).IsMCPOrigin(secret))

	// Header is not "1" even with correct secret → not trusted
	assert.False(t, (&MCPContextInput{MCPOrigin: "0", MCPWorkerSecret: secret}).IsMCPOrigin(secret))

	// Empty input → not trusted
	assert.False(t, (&MCPContextInput{}).IsMCPOrigin(secret))

	// Empty expected secret (misconfigured backend) → fail closed, never trust
	assert.False(t, (&MCPContextInput{MCPOrigin: "1", MCPWorkerSecret: "anything"}).IsMCPOrigin(""))
}
