package inputs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMCPContextInput_IsMCPOrigin(t *testing.T) {
	assert.True(t, (&MCPContextInput{MCPOrigin: "1"}).IsMCPOrigin())
	assert.False(t, (&MCPContextInput{MCPOrigin: "0"}).IsMCPOrigin())
	assert.False(t, (&MCPContextInput{}).IsMCPOrigin())
}
